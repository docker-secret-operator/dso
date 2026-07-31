package proxy

import (
	"bufio"
	"net"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testLogger(t *testing.T) *zap.Logger {
	t.Helper()
	return zap.NewNop()
}

// startEchoBackend starts a plain TCP echo server for use as a proxy backend
// in tests, and returns its address plus a stop function.
func startEchoBackend(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start echo backend: %v", err)
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		close(done)
	}
}

func TestServer_BindUnbind(t *testing.T) {
	reg := NewRegistry()
	router := NewRouter(reg)
	s := NewServer(router, testLogger(t))
	defer s.Close()

	t.Run("Bind opens a listener on an OS-assigned port", func(t *testing.T) {
		if err := s.Bind(PortBinding{ListenPort: 0, TargetPort: 80}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		bindings := s.Bindings()
		if len(bindings) != 1 {
			t.Fatalf("expected 1 binding, got %d", len(bindings))
		}
		if bindings[0].ListenPort == 0 {
			t.Error("expected ListenPort to be resolved to the real OS-assigned port")
		}
	})

	t.Run("Bind is idempotent for an already-bound port", func(t *testing.T) {
		s2 := NewServer(NewRouter(NewRegistry()), testLogger(t))
		defer s2.Close()

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()

		if err := s2.Bind(PortBinding{ListenPort: port}); err != nil {
			t.Fatalf("first bind failed: %v", err)
		}
		// Second bind on the same port should succeed as a no-op, not error.
		if err := s2.Bind(PortBinding{ListenPort: port}); err != nil {
			t.Fatalf("expected idempotent second Bind to succeed, got: %v", err)
		}
		if len(s2.Bindings()) != 1 {
			t.Errorf("expected still only 1 binding after duplicate Bind, got %d", len(s2.Bindings()))
		}
	})

	t.Run("Unbind removes an existing binding", func(t *testing.T) {
		s3 := NewServer(NewRouter(NewRegistry()), testLogger(t))
		defer s3.Close()
		if err := s3.Bind(PortBinding{ListenPort: 0}); err != nil {
			t.Fatal(err)
		}
		port := s3.Bindings()[0].ListenPort
		if err := s3.Unbind(port); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(s3.Bindings()) != 0 {
			t.Error("expected 0 bindings after Unbind")
		}
	})

	t.Run("Unbind errors for a port that was never bound", func(t *testing.T) {
		s4 := NewServer(NewRouter(NewRegistry()), testLogger(t))
		defer s4.Close()
		err := s4.Unbind(59999)
		if err == nil {
			t.Fatal("expected error unbinding a port that was never bound")
		}
	})
}

func TestServer_Close(t *testing.T) {
	s := NewServer(NewRouter(NewRegistry()), testLogger(t))
	if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
		t.Fatal(err)
	}
	port := s.Bindings()[0].ListenPort

	s.Close()

	if len(s.Bindings()) != 0 {
		t.Error("expected 0 bindings after Close")
	}
	// Confirm the OS port is actually released (best-effort: dialing should fail
	// or at least not reach our proxy anymore -- listener is closed).
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Error("expected connection to fail after Close (listener should be closed)")
	}
}

func TestServer_CloseGraceful(t *testing.T) {
	t.Run("returns nil when no in-flight connections", func(t *testing.T) {
		s := NewServer(NewRouter(NewRegistry()), testLogger(t))
		if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
			t.Fatal(err)
		}
		if err := s.CloseGraceful(2 * time.Second); err != nil {
			t.Errorf("expected graceful close with no connections to succeed, got: %v", err)
		}
	})

	t.Run("stops accepting new connections immediately", func(t *testing.T) {
		s := NewServer(NewRouter(NewRegistry()), testLogger(t))
		if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
			t.Fatal(err)
		}
		port := s.Bindings()[0].ListenPort

		if err := s.CloseGraceful(1 * time.Second); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Error("expected new connections to be refused after CloseGraceful")
		}
	})
}

// TestServer_EndToEndProxying is the core integration test for the proxy's
// actual purpose: accept a client connection, route it to a backend via the
// Router, and pipe data bidirectionally.
func TestServer_EndToEndProxying(t *testing.T) {
	backendAddr, stopBackend := startEchoBackend(t)
	defer stopBackend()

	reg := NewRegistry()
	if err := reg.Add(Backend{ID: "echo", Addr: backendAddr}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg)
	s := NewServer(router, testLogger(t))
	defer s.Close()

	if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
		t.Fatal(err)
	}
	proxyPort := s.Bindings()[0].ListenPort

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(proxyPort), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	message := "hello through the proxy\n"
	if _, err := conn.Write([]byte(message)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read echoed response through proxy: %v", err)
	}
	if line != message {
		t.Errorf("expected echoed message %q, got %q", message, line)
	}

	// Confirm the router's request counter was incremented by handleConn.
	b, _ := reg.Get("echo")
	if b.Requests() != 1 {
		t.Errorf("expected 1 request recorded on the backend, got %d", b.Requests())
	}
}

// TestServer_NoBackendAvailable confirms a client connection is dropped
// cleanly (not hung, not panicking) when the router has no active backend.
func TestServer_NoBackendAvailable(t *testing.T) {
	s := NewServer(NewRouter(NewRegistry()), testLogger(t))
	defer s.Close()

	if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
		t.Fatal(err)
	}
	proxyPort := s.Bindings()[0].ListenPort

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(proxyPort), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// With no backend, the proxy should close the connection promptly rather
	// than hang. Confirm the read either errors (EOF/reset) or times out very
	// quickly (not indefinitely).
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected read to fail (connection closed) when no backend is available")
	}
}

// TestServer_ConnectionCap proves connections beyond the configured cap are
// rejected rather than queued indefinitely, guarding against a client that
// opens connections without sending data to exhaust FDs/goroutines.
func TestServer_ConnectionCap(t *testing.T) {
	backendAddr, stopBackend := startEchoBackend(t)
	defer stopBackend()

	reg := NewRegistry()
	if err := reg.Add(Backend{ID: "echo", Addr: backendAddr}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg)
	const connCap = 2
	s := NewServerWithMaxConns(router, testLogger(t), connCap)
	defer s.Close()

	if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
		t.Fatal(err)
	}
	proxyPort := s.Bindings()[0].ListenPort
	proxyAddr := "127.0.0.1:" + strconv.Itoa(proxyPort)

	// Open `connCap` connections and hold them open (don't send/receive, so
	// they stay counted as in-flight).
	var held []net.Conn
	for i := 0; i < connCap; i++ {
		conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
		if err != nil {
			t.Fatalf("failed to open connection %d: %v", i, err)
		}
		held = append(held, conn)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	// Give handleConn goroutines time to register in the semaphore before
	// testing the connCap — otherwise this test could pass by accident if the
	// (connCap+1)th dial races ahead of the first connCap connections being counted.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.connsMu.Lock()
		n := len(s.conns)
		s.connsMu.Unlock()
		if n >= connCap {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d connections to register (got %d)", connCap, n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The next connection should be accepted at the TCP level (listener
	// backlog) but then closed immediately by the proxy once over connCap.
	extra, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial of over-connCap connection should succeed at TCP level, got: %v", err)
	}
	defer func() { _ = extra.Close() }()

	_ = extra.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16)
	_, err = extra.Read(buf)
	if err == nil {
		t.Error("expected the over-connCap connection to be closed by the proxy, but read succeeded")
	}
}

// TestServer_CloseGraceful_ForceClosesOnTimeout proves that connections still
// in-flight when the drain timeout expires are force-closed rather than
// leaked, so CloseGraceful's caller can rely on no goroutines/FDs surviving
// past its return.
func TestServer_CloseGraceful_ForceClosesOnTimeout(t *testing.T) {
	// A backend that accepts but never replies, so the proxied connection
	// stays open (in-flight) until something closes it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn // held open, never read/write/close
		}
	}()

	reg := NewRegistry()
	if err := reg.Add(Backend{ID: "stall", Addr: ln.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(reg)
	s := NewServer(router, testLogger(t))

	if err := s.Bind(PortBinding{ListenPort: 0}); err != nil {
		t.Fatal(err)
	}
	proxyPort := s.Bindings()[0].ListenPort

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(proxyPort), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect to proxy: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Wait for the connection to register as in-flight.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.connsMu.Lock()
		n := len(s.conns)
		s.connsMu.Unlock()
		if n >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for connection to register as in-flight")
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	err = s.CloseGraceful(300 * time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected CloseGraceful to report a drain timeout, since the connection never completes on its own")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("CloseGraceful took %s — should return promptly after its own timeout, not hang", elapsed)
	}

	// The defining behavior under test: the connection must actually be
	// closed, not merely reported as timed-out while left dangling.
	// handleConn's cleanup (removing itself from s.conns) runs asynchronously
	// after Close() unblocks its pipe goroutines, so poll rather than assert
	// immediately.
	drainDeadline := time.Now().Add(2 * time.Second)
	for {
		s.connsMu.Lock()
		remaining := len(s.conns)
		s.connsMu.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(drainDeadline) {
			t.Fatalf("expected 0 connections remaining after force-close, got %d", remaining)
		}
		time.Sleep(10 * time.Millisecond)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16)
	if _, err := conn.Read(buf); err == nil {
		t.Error("expected client connection to be closed by force-close on drain timeout")
	}
}

// TestIdleTimeoutConn_ClosesOnIdle proves idleTimeoutConn actually enforces
// its timeout (a Read that never gets data eventually errors) while NOT
// cutting a connection that keeps producing data within the window, using a
// short timeout directly rather than the 10-minute production constant.
func TestIdleTimeoutConn_ClosesOnIdle(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	wrapped := &idleTimeoutConn{Conn: server, timeout: 100 * time.Millisecond}

	start := time.Now()
	buf := make([]byte, 16)
	_, err := wrapped.Read(buf)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Read to time out on an idle connection")
	}
	if elapsed > 1*time.Second {
		t.Fatalf("Read took %s to time out — expected close to the configured 100ms deadline", elapsed)
	}
}

// TestIdleTimeoutConn_ResetsOnActivity proves the deadline is refreshed on
// each Read, so a connection that keeps sending data within the window is
// never cut even if its total lifetime exceeds the idle timeout.
func TestIdleTimeoutConn_ResetsOnActivity(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()

	wrapped := &idleTimeoutConn{Conn: server, timeout: 150 * time.Millisecond}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for i := 0; i < 5; i++ {
			time.Sleep(75 * time.Millisecond)
			if _, err := client.Write([]byte("x")); err != nil {
				return
			}
		}
	}()

	buf := make([]byte, 1)
	for i := 0; i < 5; i++ {
		if _, err := wrapped.Read(buf); err != nil {
			t.Fatalf("read %d failed unexpectedly (deadline should have reset on prior activity): %v", i, err)
		}
	}
	<-writeDone
}
