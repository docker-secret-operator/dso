package provider

import (
	"context"
	"net/rpc"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/hashicorp/go-plugin"
)

// GetSecretArgs carries the arguments for the deadline-aware GetSecret RPC.
// Fields must stay exported so gob can encode them across the plugin boundary.
type GetSecretArgs struct {
	Name string
	// Deadline is the caller's absolute deadline, or the zero time when the
	// caller has none. Absolute rather than a duration so the plugin isn't
	// working from a timer that started when the message was sent.
	Deadline time.Time
}

// ProviderRPC is an implementation that communicates over RPC
type ProviderRPC struct {
	client *rpc.Client
}

func (g *ProviderRPC) Init(config map[string]string) error {
	return g.client.Call("Plugin.Init", config, new(interface{}))
}

func (g *ProviderRPC) GetSecret(name string) (map[string]string, error) {
	var resp map[string]string
	err := g.client.Call("Plugin.GetSecret", name, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// GetSecretWithContext implements api.SecretProviderWithContext so that a
// caller's context can abort a fetch that the plugin is taking too long to
// answer.
//
// Why this exists: api.SecretProviderWithContext was declared and
// internal/agent/trigger.go already type-asserted for it (with a comment
// claiming agent shutdown cancels in-flight calls), but *nothing implemented
// it*. The assertion therefore always failed and every fetch took the
// blocking path, so a provider that never responded would hang the rotation
// path indefinitely -- the agent's own root context could not interrupt it.
// Implementing it here fixes that for all four external plugins at once,
// since ProviderRPC is what the daemon holds for every one of them.
//
// net/rpc has no notion of cancellation, so this cannot abort the remote work
// itself: Client.Go dispatches the call and we race its completion against
// ctx.Done(). On cancellation the caller is released immediately with
// ctx.Err() while the in-flight call is abandoned; net/rpc's own read loop
// reaps it when the plugin eventually replies, and the plugin process is
// killed on shutdown regardless. Unblocking the daemon is the point -- see
// the note on server-side deadline propagation in
// docs/audit/DECISION_LOG.md.
func (g *ProviderRPC) GetSecretWithContext(ctx context.Context, name string) (map[string]string, error) {
	// Fail fast if the caller is already done, so a cancelled context never
	// starts new remote work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Propagate the caller's deadline to the plugin so it can bound its own
	// SDK call, instead of continuing to burn a cloud API request after the
	// daemon has already given up. Older plugin binaries do not expose this
	// method; that case falls back below.
	args := GetSecretArgs{Name: name}
	if dl, ok := ctx.Deadline(); ok {
		args.Deadline = dl
	}

	var resp map[string]string
	// Buffered by net/rpc: Go() returns immediately and signals on Done.
	call := g.client.Go("Plugin.GetSecretWithDeadline", args, &resp, make(chan *rpc.Call, 1))

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case done := <-call.Done:
		if done.Error != nil {
			if isUnknownRPCMethod(done.Error) {
				// Plugin predates GetSecretWithDeadline. Fall back to the
				// original method: the caller still gets client-side
				// cancellation, just without server-side bounding.
				return g.getSecretLegacy(ctx, name)
			}
			return nil, done.Error
		}
		return resp, nil
	}
}

// getSecretLegacy is the pre-deadline call path, retained so a daemon can talk
// to a plugin binary built before GetSecretWithDeadline existed.
func (g *ProviderRPC) getSecretLegacy(ctx context.Context, name string) (map[string]string, error) {
	var resp map[string]string
	call := g.client.Go("Plugin.GetSecret", name, &resp, make(chan *rpc.Call, 1))

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case done := <-call.Done:
		if done.Error != nil {
			return nil, done.Error
		}
		return resp, nil
	}
}

// isUnknownRPCMethod reports whether err is net/rpc's "method not found"
// response, which is how an older plugin answers a method it does not
// implement. net/rpc returns this as a plain ServerError string, so matching
// on the text is the only option available.
func isUnknownRPCMethod(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "can't find method") ||
		strings.Contains(msg, "can't find service")
}

func (g *ProviderRPC) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)

	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		backoff := 2 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				val, err := g.GetSecret(name)
				var errMsg string
				if err != nil {
					// Normalize network and credential timeouts reliably
					errMsg = "Provider timeout or failure: " + err.Error()
					select {
					case ch <- api.SecretUpdate{Name: name, Data: nil, Error: errMsg}:
					case <-ctx.Done():
						return
					}

					// Apply exponential jitter gracefully locally tracking failures
					ticker.Reset(interval + backoff)
					if backoff < 60*time.Second {
						backoff *= 2
					}
					continue
				}

				// Reset ticker natively bounds correctly mapping the interval precisely back to standard limits
				ticker.Reset(interval)
				backoff = 2 * time.Second

				select {
				case ch <- api.SecretUpdate{Name: name, Data: val, Error: ""}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// ProviderRPCServer is the RPC server that ProviderRPC talks to
type ProviderRPCServer struct {
	Impl api.SecretProvider
}

func (s *ProviderRPCServer) Init(config map[string]string, resp *interface{}) error {
	return s.Impl.Init(config)
}

func (s *ProviderRPCServer) GetSecret(name string, resp *map[string]string) error {
	v, err := s.Impl.GetSecret(name)
	*resp = v
	return err
}

// GetSecretWithDeadline is the deadline-aware counterpart of GetSecret.
//
// It exists as a SEPARATE method rather than a change to GetSecret's argument
// type on purpose: net/rpc encodes arguments with gob, so widening `string` to
// a struct in place would break every already-installed plugin binary. Adding
// a method keeps old plugins working — a daemon calling this against an older
// plugin gets net/rpc's "can't find method" error, which the client detects
// and falls back from (see ProviderRPC.GetSecretWithContext).
//
// The deadline is only *honored* if the plugin implements
// api.SecretProviderWithContext; otherwise the underlying SDK call is not
// interruptible and we simply run it to completion. Passing the deadline is
// still worthwhile: it lets a context-aware plugin stop work the daemon no
// longer cares about, and an already-expired deadline is rejected before any
// remote work starts.
func (s *ProviderRPCServer) GetSecretWithDeadline(args GetSecretArgs, resp *map[string]string) error {
	ctx := context.Background()
	if !args.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, args.Deadline)
		defer cancel()

		// The daemon's deadline may already have passed in transit; don't
		// start a provider call that is guaranteed to be discarded.
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	if withCtx, ok := s.Impl.(api.SecretProviderWithContext); ok {
		v, err := withCtx.GetSecretWithContext(ctx, args.Name)
		*resp = v
		return err
	}

	v, err := s.Impl.GetSecret(args.Name)
	*resp = v
	return err
}

// WatchSecret is not directly exposed via net/rpc due to stream limit.
func (s *ProviderRPCServer) WatchSecret(args interface{}, resp *interface{}) error {
	return nil
}

// SecretProviderPlugin is the implementation of plugin.Plugin
type SecretProviderPlugin struct {
	Impl api.SecretProvider
}

func (p *SecretProviderPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &ProviderRPCServer{Impl: p.Impl}, nil
}

func (p *SecretProviderPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ProviderRPC{client: c}, nil
}

// Handshake
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "DSO_PLUGIN",
	MagicCookieValue: "hello",
}

var PluginMap = map[string]plugin.Plugin{
	"provider": &SecretProviderPlugin{},
}
