package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

// peerIdentity, peerAuthorized and readPeerIdentity live in peercred.go and the
// platform-specific peercred_linux.go / peercred_other.go files. SO_PEERCRED is
// Linux-only, so the credential read is build-tagged to keep the package
// compiling on darwin and other platforms (the agent targets Linux in production).

func prepareSocketPath(socketPath string, perm os.FileMode) error {
	if socketPath == "" {
		return fmt.Errorf("socket path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), perm); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}
	return nil
}

type AgentServer struct {
	Cache  *SecretCache
	Store  *providers.SecretStoreManager
	Logger *zap.Logger
	Config *config.Config
	Events []string // Simple in-memory event log for 'watch'
	mu     sync.Mutex
}

func (s *AgentServer) Emit(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
	if len(s.Events) > 100 {
		s.Events[0] = ""
		s.Events = s.Events[1:]
	}
}

func (s *AgentServer) GetEvents(req *api.AgentRequest, resp *api.AgentResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Logger.Debug("Streaming events to watcher")
	resp.Data = map[string]string{}
	for i, e := range s.Events {
		resp.Data[fmt.Sprintf("%d", i)] = e
	}
	return nil
}

func (s *AgentServer) GetSecret(req *api.AgentRequest, resp *api.AgentResponse) error {
	cacheKey := fmt.Sprintf("%s:%s", req.Provider, req.Secret)

	// fast path cache
	if data, found := s.Cache.Get(cacheKey); found {
		s.Logger.Debug("Cache hit", zap.String("secret", req.Secret))
		observability.SecretCacheHitsTotal.WithLabelValues(req.Secret).Inc()
		observability.SecretRequestsTotal.WithLabelValues(req.Provider, "success").Inc()
		resp.Data = data
		return nil
	}

	observability.SecretCacheMissesTotal.Inc()
	s.Logger.Info("Fetching secret from provider", zap.String("provider", req.Provider), zap.String("secret", req.Secret))

	// slow path provider lookup
	timer := observability.SecretFetchLatency.WithLabelValues(req.Provider)
	start := time.Now()

	// Find the provider config in the global config
	pName := req.Provider
	if pName == "" {
		// Fallback to legacy default if only one provider exists
		if len(s.Config.Providers) == 1 {
			for name := range s.Config.Providers {
				pName = name
				break
			}
		}
	}

	pCfg, ok := s.Config.Providers[pName]
	if !ok {
		// If not found in map, check if it's a legacy one-off request
		pCfg = config.ProviderConfig{
			Type:   pName,
			Config: req.Config,
		}
	}

	prov, err := s.Store.GetProvider(pName, pCfg)
	if err != nil {
		observability.SecretRequestsTotal.WithLabelValues(req.Provider, "error").Inc()
		observability.BackendFailuresTotal.WithLabelValues(req.Provider, "load_fail").Inc()
		resp.Error = err.Error()
		return err
	}

	type fetchResult struct {
		data map[string]string
		err  error
	}
	fetchCh := make(chan fetchResult, 1)
	go func() {
		d, e := prov.GetSecret(req.Secret)
		fetchCh <- fetchResult{d, e}
	}()

	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fetchCancel()

	var data map[string]string
	select {
	case res := <-fetchCh:
		data = res.data
		err = res.err
	case <-fetchCtx.Done():
		timer.Observe(time.Since(start).Seconds())
		observability.SecretRequestsTotal.WithLabelValues(req.Provider, "error").Inc()
		observability.BackendFailuresTotal.WithLabelValues(req.Provider, "timeout").Inc()
		resp.Error = "provider timed out after 30s"
		return fmt.Errorf("provider timed out fetching secret %q", req.Secret)
	}

	timer.Observe(time.Since(start).Seconds())
	if err != nil {
		observability.SecretRequestsTotal.WithLabelValues(req.Provider, "error").Inc()
		observability.BackendFailuresTotal.WithLabelValues(req.Provider, "fetch_fail").Inc()
		resp.Error = err.Error()
		return err
	}

	observability.SecretRequestsTotal.WithLabelValues(req.Provider, "success").Inc()

	s.Cache.Set(cacheKey, data)
	resp.Data = data
	return nil
}

// StartSocketServer starts the internal IPC RPC server on a Unix domain socket.
//
// CQ-C2: the accept loop is bound to ctx. When ctx is cancelled (SIGTERM) the
// listener is closed, which unblocks Accept and lets the loop exit; the returned
// shutdown function additionally force-closes any in-flight connections and waits
// (via a WaitGroup) for every goroutine to finish, so no goroutine survives
// shutdown.
//
// SEC-C2: every accepted connection's peer credentials (PID/UID/GID) are read via
// SO_PEERCRED, authorized against a least-privilege policy, and recorded in the
// audit log before the connection is served.
func StartSocketServer(ctx context.Context, socketPath string, cache *SecretCache, store *providers.SecretStoreManager, logger *zap.Logger, cfg *config.Config) (*AgentServer, func(), error) {
	server := &AgentServer{
		Cache:  cache,
		Store:  store,
		Logger: logger,
		Config: cfg,
	}

	if err := prepareSocketPath(socketPath, 0750); err != nil {
		return nil, nil, fmt.Errorf("failed to prepare socket path %s: %w", socketPath, err)
	}

	if err := rpc.RegisterName("Agent", server); err != nil {
		return nil, nil, fmt.Errorf("failed to register RPC service: %w", err)
	}

	// Pre-bind check: is there another agent already running?
	if _, err := os.Stat(socketPath); err == nil {
		conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("another DSO agent is already responsive on %s", socketPath)
		}
		// Stale socket, remove it
		logger.Warn("Removing stale Unix socket", zap.String("path", socketPath))
		if err := os.Remove(socketPath); err != nil {
			logger.Warn("Failed to remove stale socket", zap.String("path", socketPath), zap.Error(err))
		}
	}

	logger.Info("Starting local Unix socket", zap.String("path", socketPath))
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on socket %s: %w", socketPath, err)
	}

	// Set socket permissions: 0660 root:dso so dso group members can connect without sudo.
	// Fall back to 0600 (root only) if the dso group doesn't exist yet.
	socketMode := os.FileMode(0600)
	socketGID := -1
	if grp, err := user.LookupGroup("dso"); err == nil {
		if gid, err := strconv.Atoi(grp.Gid); err == nil {
			socketGID = gid
			socketMode = 0660
		}
	} else {
		logger.Warn("dso group not found; socket access restricted to root. Add users to 'dso' group for non-root access.")
	}
	if socketGID >= 0 {
		if err := os.Chown(filepath.Dir(socketPath), 0, socketGID); err != nil {
			logger.Warn("Failed to chown socket directory to dso group", zap.String("path", filepath.Dir(socketPath)), zap.Error(err))
		}
		if err := os.Chown(socketPath, 0, socketGID); err != nil {
			logger.Warn("Failed to chown socket to dso group", zap.String("path", socketPath), zap.Error(err))
		}
	}
	if err := os.Chmod(socketPath, socketMode); err != nil {
		_ = listener.Close()
		logger.Error("FATAL: Failed to set socket permissions. Agent cannot start securely.",
			zap.String("path", socketPath),
			zap.Error(err))
		return nil, nil, fmt.Errorf("failed to secure socket permissions: %w", err)
	}

	selfUID := os.Getuid()
	dsoGID := socketGID // -1 when the dso group does not exist

	// Track in-flight connections so the shutdown path can force-close them and
	// every serving goroutine can be awaited. rpc.ServeConn returns as soon as the
	// connection is closed, so closing tracked conns reliably unblocks them.
	var (
		wg        sync.WaitGroup
		connMu    sync.Mutex
		conns     = make(map[net.Conn]struct{})
		shutdown  bool
		closeOnce sync.Once
		stopOnce  sync.Once
		stopWatch = make(chan struct{})
	)
	closeListener := func() { closeOnce.Do(func() { _ = listener.Close() }) }
	stopWatcher := func() { stopOnce.Do(func() { close(stopWatch) }) }

	// Close the listener when ctx is cancelled (or shutdown is requested) to
	// unblock Accept(). The stopWatch channel lets shutdownFn release this
	// goroutine even if ctx has not yet been cancelled, so wg.Wait() never hangs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case <-stopWatch:
		}
		closeListener()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				// A closed listener (shutdown) surfaces here as an error; exit cleanly.
				select {
				case <-ctx.Done():
					logger.Info("IPC socket accept loop shutting down")
					return
				default:
				}
				connMu.Lock()
				closing := shutdown
				connMu.Unlock()
				if closing {
					return
				}
				logger.Error("Socket accept error", zap.Error(err))
				continue
			}

			// SEC-C2: authenticate and audit the peer before serving.
			peer, perr := readPeerIdentity(conn)
			switch {
			case errors.Is(perr, errPeerCredUnsupported):
				// Non-Linux platform: peer-credential auth is unavailable, so we
				// fall back to the socket's filesystem permissions (the same guard
				// the code had before SEC-C2). Production runs on Linux where the
				// full check applies.
				logger.Warn("Peer credential check unsupported on this platform; relying on socket file permissions")
			case perr != nil:
				logger.Warn("Rejecting IPC connection: cannot read peer credentials",
					zap.Error(perr))
				_ = conn.Close()
				continue
			case !peerAuthorized(peer, selfUID, dsoGID):
				logger.Warn("Rejecting unauthorized IPC connection",
					zap.Int32("peer_pid", peer.pid),
					zap.Uint32("peer_uid", peer.uid),
					zap.Uint32("peer_gid", peer.gid))
				_ = conn.Close()
				continue
			default:
				logger.Info("IPC connection authorized",
					zap.Int32("peer_pid", peer.pid),
					zap.Uint32("peer_uid", peer.uid),
					zap.Uint32("peer_gid", peer.gid))
			}

			connMu.Lock()
			if shutdown {
				connMu.Unlock()
				_ = conn.Close()
				continue
			}
			conns[conn] = struct{}{}
			connMu.Unlock()

			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer func() {
					connMu.Lock()
					delete(conns, c)
					connMu.Unlock()
					_ = c.Close()
				}()
				rpc.ServeConn(c)
			}(conn)
		}
	}()

	shutdownFn := func() {
		connMu.Lock()
		shutdown = true
		for c := range conns {
			_ = c.Close()
		}
		connMu.Unlock()
		stopWatcher() // release the listener-watcher goroutine if ctx is still live
		closeListener()
		wg.Wait()
	}

	return server, shutdownFn, nil
}

// ServeHTTP handles Docker V2 Secret Driver requests (POST /SecretDriver.Get)
func (s *AgentServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/SecretDriver.Get" {
		http.NotFound(w, r)
		return
	}

	var req api.DockerV2SecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.Logger.Error("Failed to decode driver request", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.Logger.Info("Docker Driver request", zap.String("name", req.Name))

	// Attempt to get from cache or default provider
	fetchReq := &api.AgentRequest{Secret: req.Name}
	fetchResp := &api.AgentResponse{}

	err := s.GetSecret(fetchReq, fetchResp)

	resp := api.DockerV2SecretResponse{}
	if err != nil || fetchResp.Error != "" {
		errorMsg := "secret not found"
		if err != nil {
			errorMsg = err.Error()
		} else if fetchResp.Error != "" {
			errorMsg = fetchResp.Error
		}
		resp.Err = errorMsg
	} else {
		valBytes, _ := json.Marshal(fetchResp.Data)
		resp.Value = valBytes
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Logger.Warn("Failed to encode driver response", zap.Error(err))
	}
}

// StartDriverServer starts the Docker V2 Secret Driver HTTP server on a Unix
// socket.
//
// CQ-C3: Serve() previously ran synchronously and blocked its caller until
// process death, with no Shutdown path. It now starts serving in an internal
// goroutine and returns immediately. The server is drained via http.Shutdown
// when ctx is cancelled, and the returned shutdown function lets the caller
// drain it explicitly on SIGTERM. The function returns only setup errors; a
// post-startup serve error is logged.
func StartDriverServer(ctx context.Context, socketPath string, cache *SecretCache, store *providers.SecretStoreManager, logger *zap.Logger, cfg *config.Config) (func(), error) {
	server := &AgentServer{
		Cache:  cache,
		Store:  store,
		Logger: logger,
		Config: cfg,
	}

	if err := prepareSocketPath(socketPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to prepare driver socket path %s: %w", socketPath, err)
	}

	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			logger.Warn("Failed to remove existing driver socket", zap.String("path", socketPath), zap.Error(err))
		}
	}

	logger.Info("Starting Docker Secret Driver socket", zap.String("path", socketPath))
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on driver socket %s: %w", socketPath, err)
	}

	// CRITICAL: Socket permissions MUST be restrictive (0600).
	// If chmod fails, the socket may be world-readable, exposing secrets.
	// Fail fast rather than silently operating with insecure permissions.
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = listener.Close()
		logger.Error("FATAL: Failed to set driver socket permissions. Agent cannot start securely.",
			zap.String("path", socketPath),
			zap.Error(err))
		return nil, fmt.Errorf("failed to secure driver socket permissions: %w", err)
	}

	httpServer := &http.Server{
		Handler:           server,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	// Serve in the background so the caller regains control immediately.
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Error("Docker Driver server error", zap.Error(serveErr))
		}
	}()

	// Drain on context cancellation.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Warn("Driver server shutdown error", zap.Error(err))
		}
	}()

	shutdownFn := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Warn("Driver server shutdown error", zap.Error(err))
		}
	}

	return shutdownFn, nil
}
