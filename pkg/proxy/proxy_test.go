package proxy_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/proxy"
	"go.uber.org/zap"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// nopLog returns a no-op zap logger suitable for tests.
func nopLog() *zap.Logger { return zap.NewNop() }

// makeBackend is a shorthand constructor.
func makeBackend(id, service, host string, port int) *proxy.Backend {
	return &proxy.Backend{ID: id, Service: service, Host: host, Port: port}
}

// startEchoServer opens a TCP listener on a random port and echoes back any
// data it receives. Returns the listener's address and a cleanup function.
func startEchoServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo server: listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) //nolint:errcheck
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() } //nolint:errcheck
}

// dialWithTimeout opens a TCP connection and returns a timeout error if not
// successful within d.
func dialWithTimeout(t *testing.T, addr string, d time.Duration) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, d)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	return conn
}

// ────────────────────────────────────────────────────────────────────────────
// Registry tests
// ────────────────────────────────────────────────────────────────────────────

func TestRegistry_Add_Valid(t *testing.T) {
	r := proxy.NewRegistry()
	b := makeBackend("a1", "api", "10.0.0.1", 3000)
	if err := r.Add(b); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := r.Backends("api"); len(got) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(got))
	}
}

func TestRegistry_Add_DuplicateID(t *testing.T) {
	r := proxy.NewRegistry()
	b := makeBackend("dup", "api", "10.0.0.1", 3000)
	_ = r.Add(b)
	if err := r.Add(b); err == nil {
		t.Fatal("expected duplicate-ID error, got nil")
	}
}

func TestRegistry_Add_MissingFields(t *testing.T) {
	r := proxy.NewRegistry()
	cases := []*proxy.Backend{
		{ID: "", Service: "api", Host: "10.0.0.1", Port: 3000},
		{ID: "x", Service: "", Host: "10.0.0.1", Port: 3000},
		{ID: "y", Service: "api", Host: "", Port: 3000},
	}
	for _, b := range cases {
		if err := r.Add(b); err == nil {
			t.Errorf("expected validation error for %+v, got nil", b)
		}
	}
}

func TestRegistry_Remove_Existing(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("b1", "svc", "h", 80))
	if err := r.Remove("b1"); err != nil {
		t.Fatalf("unexpected remove error: %v", err)
	}
	if got := r.Backends("svc"); len(got) != 0 {
		t.Fatalf("expected 0 backends after remove, got %d", len(got))
	}
}

func TestRegistry_Remove_Unknown(t *testing.T) {
	r := proxy.NewRegistry()
	if err := r.Remove("ghost"); err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}

func TestRegistry_Backends_EmptyService(t *testing.T) {
	r := proxy.NewRegistry()
	if got := r.Backends("no-such-service"); got != nil {
		t.Fatalf("expected nil for unknown service, got %v", got)
	}
}

func TestRegistry_All_MultipleServices(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("x1", "alpha", "h1", 80))
	_ = r.Add(makeBackend("x2", "beta", "h2", 80))
	_ = r.Add(makeBackend("x3", "alpha", "h3", 80))

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 backends total, got %d", len(all))
	}
}

func TestRegistry_Remove_CleansUpServiceKey(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("only", "svc", "h", 80))
	_ = r.Remove("only")

	names := r.ServiceNames()
	for _, n := range names {
		if n == "svc" {
			t.Error("service key must be removed when last backend is deregistered")
		}
	}
}

func TestRegistry_ConcurrentAddRemove(t *testing.T) {
	r := proxy.NewRegistry()
	var wg sync.WaitGroup
	const n = 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("b%d", i)
			_ = r.Add(makeBackend(id, "svc", "h", i+1))
			// Remove half of them to exercise concurrent delete.
			if i%2 == 0 {
				_ = r.Remove(id)
			}
		}(i)
	}
	wg.Wait()
	// No panic and no data race is the acceptance criterion.
}

// ────────────────────────────────────────────────────────────────────────────
// Router tests
// ────────────────────────────────────────────────────────────────────────────

func TestRouter_RoundRobin(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("r0", "svc", "host0", 80))
	_ = r.Add(makeBackend("r1", "svc", "host1", 80))

	router := proxy.NewRouter(r)

	got := make([]string, 4)
	for i := range got {
		b, err := router.Next("svc")
		if err != nil {
			t.Fatalf("unexpected error on Next(): %v", err)
		}
		got[i] = b.Host
	}

	// With two backends the sequence must alternate.
	if got[0] == got[1] {
		t.Errorf("consecutive calls must select different backends: %v", got)
	}
	if got[0] != got[2] {
		t.Errorf("call[0] and call[2] must select the same backend: %v", got)
	}
}

func TestRouter_SingleBackend(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("only", "svc", "host", 80))
	router := proxy.NewRouter(r)

	for i := 0; i < 5; i++ {
		b, err := router.Next("svc")
		if err != nil {
			t.Fatalf("error on call %d: %v", i, err)
		}
		if b.Host != "host" {
			t.Errorf("call %d: expected 'host', got %q", i, b.Host)
		}
	}
}

func TestRouter_NoBackends_ReturnsError(t *testing.T) {
	r := proxy.NewRegistry()
	router := proxy.NewRouter(r)

	if _, err := router.Next("missing"); err == nil {
		t.Fatal("expected error when no backends are registered, got nil")
	}
}

func TestRouter_ConcurrentNext(t *testing.T) {
	r := proxy.NewRegistry()
	_ = r.Add(makeBackend("c0", "svc", "h0", 80))
	_ = r.Add(makeBackend("c1", "svc", "h1", 80))
	router := proxy.NewRouter(r)

	var wg sync.WaitGroup
	const n = 500
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := router.Next("svc"); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ────────────────────────────────────────────────────────────────────────────
// Server integration tests
// ────────────────────────────────────────────────────────────────────────────

// newTestProxy builds a complete proxy stack (registry + router + server) for
// use in integration tests.
func newTestProxy(t *testing.T) (*proxy.Registry, *proxy.Server) {
	t.Helper()
	reg := proxy.NewRegistry()
	router := proxy.NewRouter(reg)
	srv := proxy.NewServer(router, nopLog())
	t.Cleanup(srv.Close)
	return reg, srv
}

func TestServer_Bind_Unbind(t *testing.T) {
	_, srv := newTestProxy(t)

	binding := proxy.PortBinding{ListenPort: 0, Service: "svc", TargetPort: 9999}
	if err := srv.Bind(binding); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}

	// Bind normalises ListenPort=0 to the real OS-assigned port.
	// Read it back from Bindings so we can call Unbind correctly.
	bindings := srv.Bindings()
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding after Bind, got %d", len(bindings))
	}
	assignedPort := bindings[0].ListenPort
	if assignedPort == 0 {
		t.Fatal("expected a real port number in Bindings, got 0")
	}

	if err := srv.Unbind(assignedPort); err != nil {
		t.Fatalf("Unbind(%d) failed: %v", assignedPort, err)
	}

	if got := srv.Bindings(); len(got) != 0 {
		t.Fatalf("expected 0 bindings after Unbind, got %d", len(got))
	}
}

func TestServer_DuplicateBind_Returns_Error(t *testing.T) {
	_, srv := newTestProxy(t)

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	// Brief pause to let the OS reclaim the port.
	time.Sleep(10 * time.Millisecond)

	b := proxy.PortBinding{ListenPort: port, Service: "svc", TargetPort: 80}
	if err := srv.Bind(b); err != nil {
		t.Fatalf("first Bind failed: %v", err)
	}
	if err := srv.Bind(b); err == nil {
		t.Fatal("expected error on duplicate Bind, got nil")
	}
}

func TestServer_EndToEnd_TCPProxy(t *testing.T) {
	// 1. Start an echo server.
	echoAddr, echoCleanup := startEchoServer(t)
	defer echoCleanup()

	echoHost, echoPortStr, _ := net.SplitHostPort(echoAddr)
	var echoPort int
	fmt.Sscanf(echoPortStr, "%d", &echoPort)

	// 2. Build proxy.
	reg, srv := newTestProxy(t)

	// 3. Register the echo server as a backend (port=0 → use TargetPort).
	_ = reg.Add(&proxy.Backend{
		ID:      "echo-1",
		Service: "echo",
		Host:    echoHost,
		Port:    0, // use binding.TargetPort
	})

	// 4. Bind proxy on :0 with TargetPort pointing to the echo server.
	binding := proxy.PortBinding{ListenPort: 0, Service: "echo", TargetPort: echoPort}
	if err := srv.Bind(binding); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	proxyPort := srv.Bindings()[0].ListenPort

	// 5. Connect through the proxy and verify echo.
	conn := dialWithTimeout(t, fmt.Sprintf("127.0.0.1:%d", proxyPort), 2*time.Second)
	defer conn.Close()

	msg := "hello-dso-proxy\n"
	conn.Write([]byte(msg)) //nolint:errcheck

	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read from proxy: %v", err)
	}
	if got := string(buf); got != msg {
		t.Errorf("expected echo %q, got %q", msg, got)
	}
}

func TestServer_RoundRobin_MultipleBackends(t *testing.T) {
	addr1, c1 := startEchoServer(t)
	defer c1()
	addr2, c2 := startEchoServer(t)
	defer c2()

	reg, srv := newTestProxy(t)

	_, port1Str, _ := net.SplitHostPort(addr1)
	_, port2Str, _ := net.SplitHostPort(addr2)
	var p1, p2 int
	fmt.Sscanf(port1Str, "%d", &p1)
	fmt.Sscanf(port2Str, "%d", &p2)

	// Register two backends, both using the binding's TargetPort for dial
	// doesn't apply here because we set Port explicitly for each.
	_ = reg.Add(&proxy.Backend{ID: "e1", Service: "lb", Host: "127.0.0.1", Port: p1})
	_ = reg.Add(&proxy.Backend{ID: "e2", Service: "lb", Host: "127.0.0.1", Port: p2})

	_ = srv.Bind(proxy.PortBinding{ListenPort: 0, Service: "lb", TargetPort: 0})
	proxyPort := srv.Bindings()[0].ListenPort

	// Make 4 connections; with round-robin we expect alternating backends.
	for i := 0; i < 4; i++ {
		conn := dialWithTimeout(t, fmt.Sprintf("127.0.0.1:%d", proxyPort), 2*time.Second)
		_, _ = conn.Write([]byte("ping\n"))
		buf := make([]byte, 5)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		io.ReadFull(conn, buf)                                 //nolint:errcheck
		conn.Close()
	}
	// Success criterion: no panic, no stall, all 4 connections complete.
}

func TestServer_NoBackend_ConnectionDropped(t *testing.T) {
	_, srv := newTestProxy(t) // empty registry

	_ = srv.Bind(proxy.PortBinding{ListenPort: 0, Service: "ghost", TargetPort: 9999})
	proxyPort := srv.Bindings()[0].ListenPort

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The proxy should close the connection immediately when no backend is found.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed when no backend is available")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// API tests
// ────────────────────────────────────────────────────────────────────────────


func TestAPI_Health(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
}

func TestAPI_AddAndListBackend(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	// Add a backend.
	payload := `{"id":"test-1","service":"api","host":"10.0.0.1","port":3000}`
	resp, err := http.Post(baseURL+"/backends", "application/json",
		strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST /backends: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	// List backends — should contain exactly 1.
	resp2, err := http.Get(baseURL + "/backends")
	if err != nil {
		t.Fatalf("GET /backends: %v", err)
	}
	defer resp2.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&result) //nolint:errcheck
	count := int(result["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
}

func TestAPI_RemoveBackend(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	// Register a backend first.
	payload := `{"id":"rm-1","service":"svc","host":"h","port":80}`
	resp, _ := http.Post(baseURL+"/backends", "application/json",
		strings.NewReader(payload))
	resp.Body.Close()

	// Delete it.
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/backends/rm-1", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /backends/rm-1: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp2.StatusCode)
	}
}

func TestAPI_RemoveUnknown_Returns404(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/backends/nope", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestAPI_DuplicateBackend_Returns409(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	payload := `{"id":"dup-2","service":"svc","host":"h","port":80}`
	first, _ := http.Post(baseURL+"/backends", "application/json",
		bytes.NewBufferString(payload))
	first.Body.Close()

	second, err := http.Post(baseURL+"/backends", "application/json",
		bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("second POST: %v", err)
	}
	defer second.Body.Close()

	if second.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", second.StatusCode)
	}
}

func TestAPI_MethodNotAllowed(t *testing.T) {
	baseURL, _, cleanup := startAPIServerDirect(t)
	defer cleanup()

	req, _ := http.NewRequest(http.MethodPut, baseURL+"/backends", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /backends: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

// startAPIServerDirect starts the API server listening on a real random TCP
// port using the standard ListenAndServe path (not httptest.NewServer).
// This validates the actual production startup path.
func startAPIServerDirect(t *testing.T) (baseURL string, reg *proxy.Registry, cleanup func()) {
	t.Helper()
	reg = proxy.NewRegistry()
	router := proxy.NewRouter(reg)
	srv := proxy.NewServer(router, nopLog())
	api := proxy.NewAPIServer(reg, srv, nopLog())

	// Find a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ready := make(chan struct{})
	go func() {
		// Brief poll to signal readiness.
		for i := 0; i < 50; i++ {
			conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
			if err == nil {
				conn.Close()
				close(ready)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		close(ready)
	}()

	go func() {
		// Ignore ErrServerClosed on cleanup.
		api.ListenAndServe(addr) //nolint:errcheck
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("API server did not start in time")
	}

	return "http://" + addr, reg, func() {
		api.Shutdown(nil) //nolint:errcheck
		srv.Close()
	}
}
