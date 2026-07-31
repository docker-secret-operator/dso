package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	dialTimeout = 5 * time.Second

	// idleTimeout bounds how long a proxied connection may go without any
	// data in either direction. It resets on every read, so long-lived but
	// active connections (e.g. a held-open database connection) are never
	// cut — only truly idle ones are, which also bounds how long a
	// connection that never sends data can pin a goroutine/FD pair.
	idleTimeout = 10 * time.Minute

	// maxConcurrentConns caps in-flight proxied connections across all
	// listeners so a client that opens connections without sending data
	// can't exhaust file descriptors/goroutines. Connections beyond the
	// cap are closed immediately after accept rather than queued.
	maxConcurrentConns = 4096

	// maxAcceptRetryDelay caps the backoff applied after a transient Accept
	// error so a sustained failure (e.g. FD exhaustion) degrades into a
	// slow retry loop instead of a tight, log-flooding busy loop.
	maxAcceptRetryDelay = 1 * time.Second
)

var dialer = &net.Dialer{
	Timeout:   dialTimeout,
	KeepAlive: 30 * time.Second,
}

// idleTimeoutConn resets a read deadline on every Read call, so the
// underlying connection is only closed by the deadline when it goes
// idle for the full duration — not merely because it has been open a while.
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(p)
}

// CloseWrite forwards to the wrapped connection's half-close if it supports
// one. net.Conn does not declare CloseWrite, so embedding alone would not
// promote it — without this, closeWrite's halfCloser type assertion would
// always miss and every direction would take a full Close instead of a
// graceful half-close, changing behavior for protocols relying on it
// (e.g. HTTP/1.1 request bodies that half-close before reading the response).
func (c *idleTimeoutConn) CloseWrite() error {
	type halfCloser interface {
		CloseWrite() error
	}
	if hc, ok := c.Conn.(halfCloser); ok {
		return hc.CloseWrite()
	}
	return c.Close()
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

	connsMu sync.Mutex
	conns   map[net.Conn]*connLegs
	connSem chan struct{}
}

// connLegs tracks both sides of one proxied connection (client and, once
// dialed, upstream) so a forced shutdown can close whichever leg the pipe
// goroutines are currently blocked reading from. Closing only the client
// leg is not sufficient: if the upstream is the stalled side, the goroutine
// copying upstream->client stays blocked in Read on the upstream socket
// and never notices the client closed.
type connLegs struct {
	mu   sync.Mutex
	legs []net.Conn
}

func (c *connLegs) add(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.legs = append(c.legs, conn)
}

func (c *connLegs) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.legs {
		_ = conn.Close()
	}
}

func NewServer(router *Router, log *zap.Logger) *Server {
	return NewServerWithMaxConns(router, log, maxConcurrentConns)
}

// NewServerWithMaxConns is like NewServer but with an explicit connection
// cap, primarily so tests can exercise the cap without opening thousands of
// connections.
func NewServerWithMaxConns(router *Router, log *zap.Logger, maxConns int) *Server {
	return &Server{
		router:    router,
		log:       log,
		listeners: make(map[int]*portListener),
		conns:     make(map[net.Conn]*connLegs),
		connSem:   make(chan struct{}, maxConns),
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
	_ = pl.listener.Close()
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
		_ = pl.listener.Close()
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
		_ = pl.listener.Close()
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
		// Drain window expired — force-close whatever is left rather than
		// leaving those goroutines/FDs to leak past this call's return.
		s.connsMu.Lock()
		remaining := len(s.conns)
		for _, legs := range s.conns {
			legs.closeAll()
		}
		s.connsMu.Unlock()
		return fmt.Errorf("proxy: drain timeout (%s), force-closed %d remaining connection(s)", timeout, remaining)
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
	retryDelay := 10 * time.Millisecond
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
				time.Sleep(retryDelay)
				if retryDelay < maxAcceptRetryDelay {
					retryDelay *= 2
					if retryDelay > maxAcceptRetryDelay {
						retryDelay = maxAcceptRetryDelay
					}
				}
				continue
			}
		}
		retryDelay = 10 * time.Millisecond

		select {
		case s.connSem <- struct{}{}:
		default:
			s.log.Warn("proxy: connection limit reached — rejecting",
				zap.Int("port", pl.binding.ListenPort),
				zap.Int("limit", maxConcurrentConns))
			_ = conn.Close()
			continue
		}

		legs := &connLegs{}
		legs.add(conn)

		s.activeConns.Add(1)
		s.connsMu.Lock()
		s.conns[conn] = legs
		s.connsMu.Unlock()
		go s.handleConn(conn, legs)
	}
}

func (s *Server) handleConn(client net.Conn, legs *connLegs) {
	defer func() {
		s.connsMu.Lock()
		delete(s.conns, client)
		s.connsMu.Unlock()
		<-s.connSem
		s.activeConns.Done()
		_ = client.Close()
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
	defer func() { _ = upstream.Close() }()
	legs.add(upstream)

	s.log.Debug("proxy: connection established",
		zap.String("client", client.RemoteAddr().String()),
		zap.String("upstream", backend.Addr),
		zap.String("backend_id", backend.ID))

	pipe(&idleTimeoutConn{Conn: client, timeout: idleTimeout}, &idleTimeoutConn{Conn: upstream, timeout: idleTimeout})
}

func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		closeWrite(b)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		closeWrite(a)
	}()
	wg.Wait()
}

func closeWrite(conn net.Conn) {
	type halfCloser interface {
		CloseWrite() error
	}
	if hc, ok := conn.(halfCloser); ok {
		_ = hc.CloseWrite()
	} else {
		_ = conn.Close()
	}
}
