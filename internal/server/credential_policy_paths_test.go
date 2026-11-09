package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// F-E1 startup-path coverage. Every production REST entry point is
// StartRESTServerSupervised (called by `dso agent`, internal/cli/agent.go) or
// the StartRESTServer wrapper that delegates to it; these tests drive both
// real listeners and check the default and the opt-in.

func startLoopback(t *testing.T, supervised bool, logger *zap.Logger) (string, func()) {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", "")
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	ctx, cancel := context.WithCancel(context.Background())
	var shutdown func()
	var err error
	if supervised {
		shutdown, _, err = StartRESTServerSupervised(ctx, "127.0.0.1:0", nil, nil, nil, logger, nil)
	} else {
		shutdown, err = StartRESTServer(ctx, "127.0.0.1:0", nil, nil, nil, logger, nil)
	}
	if err != nil {
		cancel()
		t.Fatalf("start (supervised=%v): %v", supervised, err)
	}
	return "http://" + (*lnp).Addr().String(), func() { shutdown(); cancel() }
}

func getStatus(t *testing.T, url string) int {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func securityWarnings(logs *observer.ObservedLogs) int {
	n := 0
	for _, e := range logs.All() {
		if e.Level == zapcore.WarnLevel && strings.HasPrefix(e.Message, "SECURITY: "+envAllowUnauthenticatedLoopback) {
			n++
		}
	}
	return n
}

// The non-supervised StartRESTServer wrapper fails closed by default too.
func TestStartRESTServerWrapper_NoTokenLoopback_FailsClosed(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "")
	core, logs := observer.New(zapcore.DebugLevel)
	base, stop := startLoopback(t, false, zap.New(core))
	defer stop()

	if code := getStatus(t, base+"/api/secrets"); code != http.StatusUnauthorized {
		t.Errorf("anonymous /api/secrets via StartRESTServer = %d, want 401", code)
	}
	if n := securityWarnings(logs); n != 0 {
		t.Errorf("SECURITY opt-in warning logged %d times without the opt-in", n)
	}
}

// With the explicit opt-in, both entry points restore the legacy loopback
// mode AND log the SECURITY warning at startup.
func TestStartRESTServer_OptIn_AllowsLoopbackAndLogsSecurityWarning(t *testing.T) {
	for _, supervised := range []bool{true, false} {
		name := "StartRESTServer"
		if supervised {
			name = "StartRESTServerSupervised"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv(envAllowUnauthenticatedLoopback, "1")
			core, logs := observer.New(zapcore.DebugLevel)
			base, stop := startLoopback(t, supervised, zap.New(core))
			defer stop()

			// nil cache/config make the handler itself error; what matters
			// here is that the auth gate let the anonymous request through.
			if code := getStatus(t, base+"/api/secrets"); code == http.StatusUnauthorized || code == http.StatusForbidden {
				t.Errorf("opted-in anonymous /api/secrets = %d, want it past the auth gate", code)
			}
			if n := securityWarnings(logs); n != 1 {
				t.Errorf("expected exactly one SECURITY opt-in warning, got %d", n)
			}
		})
	}
}
