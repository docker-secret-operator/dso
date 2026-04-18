package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PortBinding associates a host-side TCP listen port with a DSO service and
// the container-side port that traffic should be forwarded to.
//
// Example: a Compose port mapping "3000:8080" produces:
//
//	PortBinding{ListenPort: 3000, Service: "api", TargetPort: 8080}
type PortBinding struct {
	// ListenPort is the port the proxy binds on the host (the left side of the
	// Compose port mapping, e.g. 3000 in "3000:8080").
	ListenPort int `json:"listen_port"`

	// Service is the DSO service name. Used to look up backends in the registry.
	Service string `json:"service"`

	// TargetPort is the port on the backend container to forward connections to
	// (the right side of the Compose port mapping, e.g. 8080 in "3000:8080").
	// This value is used when Backend.Port is 0, which is the common case for
	// homogeneous replica sets where all instances listen on the same port.
	TargetPort int `json:"target_port"`
}

// dialTimeout is the maximum time allowed to establish an upstream connection.
const dialTimeout = 5 * time.Second

// dialer is used for all upstream connections. The KeepAlive setting detects
// silently dead TCP connections (e.g. container crash without FIN).
var dialer = &net.Dialer{
	Timeout:   dialTimeout,
	KeepAlive: 30 * time.Second,
}

// portListener bundles a net.Listener with the metadata of its PortBinding.
type portListener struct {
	binding  PortBinding
	listener net.Listener
	done     chan struct{} // closed by Unbind/Close to signal the accept loop
}

// Server is the core DSO TCP proxy. It owns one TCP listener per PortBinding
// and routes each accepted connection to a backend chosen by the Router.
//
// Lifecycle:
//   - Call Bind to start listening on a port.
//   - Call Unbind to stop a specific listener without affecting others.
//   - Call Close to tear down all listeners at once.
//
// All methods are safe for concurrent use.
type Server struct {
	router *Router
	log    *zap.Logger

	mu        sync.RWMutex
	listeners map[int]*portListener // listenPort → active listener
}

// NewServer creates a new proxy server backed by the given router.
// The logger is used for connection-level diagnostics.
func NewServer(router *Router, log *zap.Logger) *Server {
	return &Server{
		router:    router,
		log:       log,
		listeners: make(map[int]*portListener),
	}
}

// Bind starts a TCP listener for the given PortBinding.
//
// Accepted connections are handled asynchronously; Bind returns as soon as the
// listener is successfully opened. Returns an error if the port is already
// bound or if the OS rejects the listen call.
func (s *Server) Bind(b PortBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.listeners[b.ListenPort]; exists {
		return fmt.Errorf("proxy: port %d is already bound", b.ListenPort)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", b.ListenPort))
	if err != nil {
		return fmt.Errorf("proxy: cannot listen on :%d: %w", b.ListenPort, err)
	}

	// When ListenPort is 0 the OS picks a free port; record the real one so
	// that Unbind can use it as the map key.
	actualPort := ln.Addr().(*net.TCPAddr).Port
	b.ListenPort = actualPort

	pl := &portListener{
		binding:  b,
		listener: ln,
		done:     make(chan struct{}),
	}
	s.listeners[actualPort] = pl

	go s.acceptLoop(pl)

	s.log.Info("proxy: port bound",
		zap.Int("listen_port", b.ListenPort),
		zap.String("service", b.Service),
		zap.Int("target_port", b.TargetPort))
	return nil
}

// Unbind stops the listener on the given host port and drains its accept loop.
// In-flight connections are not terminated — they run to completion.
func (s *Server) Unbind(listenPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pl, exists := s.listeners[listenPort]
	if !exists {
		return fmt.Errorf("proxy: port %d is not bound", listenPort)
	}

	close(pl.done)
	pl.listener.Close()
	delete(s.listeners, listenPort)

	s.log.Info("proxy: port unbound", zap.Int("port", listenPort))
	return nil
}

// Close shuts down all active listeners. In-flight connections continue until
// they complete; only new accept() calls are rejected.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for port, pl := range s.listeners {
		close(pl.done)
		pl.listener.Close()
		delete(s.listeners, port)
	}
	s.log.Info("proxy: all listeners closed")
}

// Bindings returns a point-in-time snapshot of all active PortBindings.
// Each returned binding's ListenPort is the real OS-assigned port (never 0).
func (s *Server) Bindings() []PortBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]PortBinding, 0, len(s.listeners))
	for _, pl := range s.listeners {
		out = append(out, pl.binding)
	}
	return out
}

// ── Accept loop ───────────────────────────────────────────────────────────────

func (s *Server) acceptLoop(pl *portListener) {
	for {
		conn, err := pl.listener.Accept()
		if err != nil {
			select {
			case <-pl.done:
				// Clean shutdown — exit silently.
				return
			default:
				s.log.Warn("proxy: accept error",
					zap.Int("port", pl.binding.ListenPort),
					zap.Error(err))
				// Brief sleep to avoid a tight loop on persistent accept errors
				// (e.g. "too many open files").
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		go s.handleConn(conn, pl.binding)
	}
}

// handleConn selects a backend, dials it, and runs the bidirectional pipe
// until both sides close. All errors are logged; the function never panics.
func (s *Server) handleConn(client net.Conn, binding PortBinding) {
	defer client.Close()

	// Select the next backend via the router.
	backend, err := s.router.Next(binding.Service)
	if err != nil {
		s.log.Warn("proxy: no backend — dropping connection",
			zap.String("service", binding.Service),
			zap.String("client", client.RemoteAddr().String()),
			zap.Error(err))
		return
	}

	// Determine the dial port.
	// Phase 1: use binding.TargetPort (uniform replicas).
	// Phase 2: if backend.Port > 0 it overrides the binding (heterogeneous replicas).
	dialPort := binding.TargetPort
	if backend.Port > 0 {
		dialPort = backend.Port
	}
	dialAddr := fmt.Sprintf("%s:%d", backend.Host, dialPort)

	upstream, err := dialer.Dial("tcp", dialAddr)
	if err != nil {
		s.log.Warn("proxy: cannot reach backend",
			zap.String("addr", dialAddr),
			zap.String("service", binding.Service),
			zap.Error(err))
		return
	}
	defer upstream.Close()

	s.log.Debug("proxy: connection proxied",
		zap.String("client", client.RemoteAddr().String()),
		zap.String("upstream", dialAddr),
		zap.String("service", binding.Service))

	pipe(client, upstream)
}

// ── Bidirectional pipe ────────────────────────────────────────────────────────

// pipe copies data concurrently between two network connections. It blocks
// until both directions are fully drained and EOF-terminated.
//
// TCP half-close semantics: when the client sends EOF on its write side
// (e.g. end of HTTP request in HTTP/1.0), we propagate that to the upstream
// via CloseWrite so the upstream can send its response and then close cleanly.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(b, a) //nolint:errcheck // errors are expected on close
		closeWrite(b)
	}()
	go func() {
		defer wg.Done()
		io.Copy(a, b) //nolint:errcheck
		closeWrite(a)
	}()

	wg.Wait()
}

// closeWrite performs a TCP half-close on the write side of conn.
// For connection types that do not support half-close (e.g. TLS), it falls
// back to a full Close(), which is acceptable because the other goroutine in
// pipe() will also finish shortly.
func closeWrite(conn net.Conn) {
	type halfCloser interface {
		CloseWrite() error
	}
	if hc, ok := conn.(halfCloser); ok {
		hc.CloseWrite() //nolint:errcheck
	} else {
		conn.Close() //nolint:errcheck
	}
}
