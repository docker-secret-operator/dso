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
