package server

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// captureListener injects the listener factory so tests know the bound
// address without reserving a fixed port.
type captureListener struct {
	net.Listener
	acceptErr error
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
	if _, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		t.Error("listener still open after shutdown")
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
