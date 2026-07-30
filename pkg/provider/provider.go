package provider

import (
	"context"
	"net/rpc"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/hashicorp/go-plugin"
)

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

	var resp map[string]string
	// Buffered by net/rpc: Go() returns immediately and signals on Done.
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
