package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

const dialTimeout = 5 * time.Second

var dialer = &net.Dialer{
	Timeout:   dialTimeout,
	KeepAlive: 30 * time.Second,
}

// PortBinding describes one host port the proxy should own.
type PortBinding struct {
	ListenPort int // host-side port
	TargetPort int // container-side port (used when backend Addr has no explicit port)
}

type portListener struct {
	binding  PortBinding
	listener net.Listener
	done     chan struct{}
}

// Server owns one TCP listener per PortBinding and routes accepted connections
// to backends chosen by the Router. All public methods are safe for concurrent use.
type Server struct {
	router *Router
	log    *zap.Logger

	mu          sync.RWMutex
	listeners   map[int]*portListener
	activeConns sync.WaitGroup
}

func NewServer(router *Router, log *zap.Logger) *Server {
	return &Server{
		router:    router,
		log:       log,
		listeners: make(map[int]*portListener),
	}
}

// Bind opens a TCP listener for the given PortBinding.
// Idempotent: if the port is already managed by this server, returns nil immediately
// so callers may safely call Bind (via EnsurePort) multiple times for the same port.
func (s *Server) Bind(b PortBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check before net.Listen — the OS would reject a second Listen on the same
	// port and we'd incorrectly skip RegisterContainer in the caller.
	if _, exists := s.listeners[b.ListenPort]; exists {
		return nil
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", b.ListenPort))
	if err != nil {
		return fmt.Errorf("proxy: listen :%d: %w", b.ListenPort, err)
	}

	realPort := ln.Addr().(*net.TCPAddr).Port
	b.ListenPort = realPort

	pl := &portListener{binding: b, listener: ln, done: make(chan struct{})}
	s.listeners[realPort] = pl
	go s.acceptLoop(pl)

	s.log.Info("proxy: port bound", zap.Int("port", realPort))
	return nil
}

// Unbind stops the listener on listenPort. In-flight connections run to completion.
func (s *Server) Unbind(listenPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pl, exists := s.listeners[listenPort]
	if !exists {
		return fmt.Errorf("proxy: port %d not bound", listenPort)
	}
	close(pl.done)
	pl.listener.Close()
	delete(s.listeners, listenPort)
	s.log.Info("proxy: port unbound", zap.Int("port", listenPort))
	return nil
}

// Close shuts down all active listeners immediately.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for port, pl := range s.listeners {
		close(pl.done)
		pl.listener.Close()
		delete(s.listeners, port)
	}
	s.log.Info("proxy: all ports closed")
}

// CloseGraceful stops accepting new connections then waits up to timeout for
// all in-flight connections to complete.
func (s *Server) CloseGraceful(timeout time.Duration) error {
	s.mu.Lock()
	for port, pl := range s.listeners {
		close(pl.done)
		pl.listener.Close()
		delete(s.listeners, port)
	}
	s.mu.Unlock()

	s.log.Info("proxy: listeners closed — draining in-flight connections",
		zap.Duration("timeout", timeout))

	done := make(chan struct{})
	go func() {
		s.activeConns.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("proxy: all connections drained")
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("proxy: drain timeout (%s)", timeout)
	}
}

// Bindings returns a snapshot of currently active PortBindings.
func (s *Server) Bindings() []PortBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]PortBinding, 0, len(s.listeners))
	for _, pl := range s.listeners {
		out = append(out, pl.binding)
	}
	return out
}

func (s *Server) acceptLoop(pl *portListener) {
	for {
		conn, err := pl.listener.Accept()
		if err != nil {
			select {
			case <-pl.done:
				return
			default:
				s.log.Warn("proxy: accept error",
					zap.Int("port", pl.binding.ListenPort),
					zap.Error(err))
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		s.activeConns.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(client net.Conn) {
	defer func() {
		s.activeConns.Done()
		client.Close()
	}()

	backend, err := s.router.Next()
	if err != nil {
		s.log.Warn("proxy: no backend — dropping connection",
			zap.String("client", client.RemoteAddr().String()),
			zap.Error(err))
		return
	}

	upstream, err := dialer.Dial("tcp", backend.Addr)
	if err != nil {
		s.log.Warn("proxy: dial backend failed",
			zap.String("addr", backend.Addr),
			zap.Error(err))
		return
	}
	defer upstream.Close()

	s.log.Debug("proxy: connection established",
		zap.String("client", client.RemoteAddr().String()),
		zap.String("upstream", backend.Addr),
		zap.String("backend_id", backend.ID))

	pipe(client, upstream)
}

func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(b, a) //nolint:errcheck
		closeWrite(b)
	}()
	go func() {
		defer wg.Done()
		io.Copy(a, b) //nolint:errcheck
		closeWrite(a)
	}()
	wg.Wait()
}

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
