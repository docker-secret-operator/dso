package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// captureListener injects the listener factory so tests know the bound
// address without reserving a fixed port.
type captureListener struct {
	net.Listener
	acceptErr error
	closed    atomic.Bool
}

// Close records that the listener was released, then closes it.
func (c *captureListener) Close() error {
	c.closed.Store(true)
	return c.Listener.Close()
}

func (c *captureListener) Accept() (net.Conn, error) {
	if c.acceptErr != nil {
		return nil, c.acceptErr
	}
	return c.Listener.Accept()
}

func injectListener(t *testing.T, wrap func(net.Listener) *captureListener) **captureListener {
	t.Helper()
	var got *captureListener
	old := listenFunc
	listenFunc = func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		got = wrap(ln)
		return got, nil
	}
	t.Cleanup(func() { listenFunc = old })
	return &got
}

func startForTest(t *testing.T, ctx context.Context, addr string) (func(), <-chan error, error) {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", "")
	return StartRESTServerSupervised(ctx, addr, nil, nil, nil, zap.NewNop(), nil)
}

func TestRESTSupervised_StartsAndShutsDownGracefully(t *testing.T) {
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := (*lnp).Addr().String()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("API not accepting connections after a nil error: %v", err)
	}
	_ = c.Close()

	shutdown()
	shutdown() // idempotent: must not panic or block
	// Contract: the listening socket is closed BEFORE shutdown() returns, in every
	// ordering of shutdown() and the Serve goroutine. Asserted synchronously.
	if !(*lnp).closed.Load() {
		t.Error("listener still open after shutdown() returned")
	}
	select {
	case e := <-failed:
		t.Fatalf("intentional shutdown must not be reported as a failure: %v", e)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRESTSupervised_AddressInUseIsReturnedSynchronously(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	shutdown, failed, err := startForTest(t, context.Background(), busy.Addr().String())
	if err == nil {
		shutdown()
		t.Fatal("binding an address that is in use must fail")
	}
	if !strings.Contains(err.Error(), "cannot listen") {
		t.Errorf("unhelpful error: %v", err)
	}
	if shutdown != nil || failed != nil {
		t.Error("no handles may be returned on failure")
	}
}

func TestRESTSupervised_InvalidAddressIsRejected(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:notaport", "127.0.0.1:99999", "not an address"} {
		if shutdown, _, err := startForTest(t, context.Background(), addr); err == nil {
			shutdown()
			t.Errorf("%q: expected an error", addr)
		}
	}
}

func TestRESTSupervised_UnexpectedListenerFailureIsReported(t *testing.T) {
	injectListener(t, func(l net.Listener) *captureListener {
		return &captureListener{Listener: l, acceptErr: errors.New("synthetic accept failure")}
	})
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	select {
	case e := <-failed:
		if e == nil || !strings.Contains(e.Error(), "synthetic accept failure") {
			t.Errorf("failure cause lost: %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an unexpected listener failure was never reported to the supervisor")
	}
}

func TestRESTSupervised_ListenerClosedUnderneathIsReported(t *testing.T) {
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	_ = (*lnp).Listener.Close() // something other than shutdown() kills the socket
	select {
	case e := <-failed:
		if e == nil {
			t.Error("nil failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener death not reported")
	}
}

func TestRESTSupervised_ContextCancelStopsServerWithoutFailure(t *testing.T) {
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	ctx, cancel := context.WithCancel(context.Background())
	_, failed, err := startForTest(t, ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := (*lnp).Addr().String()
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server still accepting after context cancellation")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case e := <-failed:
		t.Fatalf("cancellation reported as failure: %v", e)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRESTSupervised_PrecheckErrorsStillReturnedAndLeaveNothingBound(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "")
	bound := false
	old := listenFunc
	listenFunc = func(n, a string) (net.Listener, error) { bound = true; return net.Listen(n, a) }
	defer func() { listenFunc = old }()
	if _, _, err := StartRESTServerSupervised(context.Background(), "0.0.0.0:0", nil, nil, nil, zap.NewNop(), nil); err == nil {
		t.Fatal("non-loopback without auth must be refused")
	}
	if bound {
		t.Error("the listener must not be created when pre-checks fail")
	}
}

// holdServe makes Serve wait on gate before it starts (so shutdown() can be made to
// run before Serve registers the listener) and optionally wraps the handler.
func holdServe(t *testing.T, gate <-chan struct{}, wrap func(http.Handler) http.Handler) <-chan error {
	t.Helper()
	old := serveFunc
	served := make(chan error, 1)
	serveFunc = func(srv *http.Server, ln net.Listener) error {
		if gate != nil {
			<-gate
		}
		if wrap != nil {
			srv.Handler = wrap(srv.Handler)
		}
		err := srv.Serve(ln)
		served <- err
		return err
	}
	t.Cleanup(func() { serveFunc = old })
	return served
}

func noFailure(t *testing.T, failed <-chan error) {
	t.Helper()
	select {
	case e := <-failed:
		t.Fatalf("unexpected failure reported: %v", e)
	case <-time.After(200 * time.Millisecond):
	}
}

// 1 + 4: shutdown immediately after startup; listener closed when it returns.
func TestRESTSupervised_ImmediateShutdownClosesListenerBeforeReturn(t *testing.T) {
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	shutdown()
	if !(*lnp).closed.Load() {
		t.Fatal("listener not closed when shutdown() returned")
	}
	noFailure(t, failed)
}

// 2: shutdown() runs strictly before Serve registers the listener. Without the
// listener close in shutdown() the socket stays open until Serve eventually runs.
func TestRESTSupervised_ShutdownBeforeServeRegistersListener(t *testing.T) {
	gate := make(chan struct{})
	served := holdServe(t, gate, nil)
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	shutdown() // Serve has not started: Shutdown alone has nothing to close
	if !(*lnp).closed.Load() {
		close(gate)
		t.Fatal("listener still open when shutdown() returned (Serve had not registered it yet)")
	}
	close(gate) // Serve now starts against a shut-down server and a closed listener
	select {
	case e := <-served:
		if !errors.Is(e, http.ErrServerClosed) {
			t.Fatalf("Serve after shutdown: %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
	noFailure(t, failed)
}

// 3: normal shutdown after Serve is accepting.
func TestRESTSupervised_ShutdownAfterServeBegan(t *testing.T) {
	served := holdServe(t, nil, nil)
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", (*lnp).Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	shutdown()
	if !(*lnp).closed.Load() {
		t.Fatal("listener not closed when shutdown() returned")
	}
	if e := <-served; !errors.Is(e, http.ErrServerClosed) {
		t.Fatalf("Serve: %v", e)
	}
	noFailure(t, failed)
}

// 5 + 8: repeated shutdown, concurrent shutdown, and an already-closed listener
// (closed by Serve's own path / by the first shutdown) produce no panic, no block
// and nothing on the failure channel.
func TestRESTSupervised_RepeatedAndConcurrentShutdownIsQuiet(t *testing.T) {
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); shutdown() }()
	}
	wg.Wait()
	shutdown()
	if !(*lnp).closed.Load() {
		t.Fatal("listener not closed")
	}
	if err := (*lnp).Listener.Close(); err == nil {
		t.Error("precondition: a second close of the socket is expected to return an error (which shutdown must swallow)")
	}
	noFailure(t, failed)
}

// 6: a request in flight when shutdown starts completes with 200; shutdown waits
// for it (graceful), and the listener is closed when shutdown() returns.
func TestRESTSupervised_ShutdownLetsInFlightRequestFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	holdServe(t, nil, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			w.WriteHeader(http.StatusOK)
		})
	})
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	status := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + (*lnp).Addr().String() + "/")
		if err != nil {
			status <- -1
			return
		}
		_ = resp.Body.Close()
		status <- resp.StatusCode
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached the handler")
	}
	returned := make(chan struct{})
	go func() { shutdown(); close(returned) }()
	select {
	case <-returned:
		t.Fatal("shutdown() returned while a request was still running (not graceful)")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown() did not return after the request finished")
	}
	if got := <-status; got != http.StatusOK {
		t.Fatalf("in-flight request: status %d, want 200", got)
	}
	if !(*lnp).closed.Load() {
		t.Fatal("listener not closed when shutdown() returned")
	}
	noFailure(t, failed)
}

// 7: when the grace period ends with a request still running, shutdown() returns
// promptly after the timeout, the listener is closed anyway (no new connection is
// accepted) and the straggling request is NOT force-closed by shutdown.
func TestRESTSupervised_ShutdownTimeoutStillClosesListener(t *testing.T) {
	old := restShutdownTimeout
	restShutdownTimeout = 150 * time.Millisecond
	t.Cleanup(func() { restShutdownTimeout = old })
	started := make(chan struct{})
	release := make(chan struct{})
	holdServe(t, nil, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
		})
	})
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	shutdown, failed, err := startForTest(t, context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if resp, err := http.Get("http://" + (*lnp).Addr().String() + "/"); err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached the handler")
	}
	t0 := time.Now()
	shutdown()
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("shutdown() took %v with a 150ms grace period", d)
	}
	if !(*lnp).closed.Load() {
		t.Fatal("listener not closed after the shutdown timeout")
	}
	close(release)
	noFailure(t, failed)
}
