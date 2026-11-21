package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunAPILogs_ConnectionFailureIsReturnedNotPrinted(t *testing.T) {
	// 127.0.0.1:1 is loopback (passes validateLoopbackURL) and refuses connections.
	var out bytes.Buffer
	err := runAPILogs(context.Background(), &out, "http://127.0.0.1:1", false)
	if err == nil || !strings.Contains(err.Error(), "cannot reach agent API") {
		t.Fatalf("want connection error, got %v", err)
	}
	if strings.Contains(out.String(), "\x1b") && colorOff() {
		t.Error("ANSI in output while colour is disabled")
	}
}

func TestRunAPILogs_RejectsNonLoopbackBeforeAnyRequest(t *testing.T) {
	var out bytes.Buffer
	if err := runAPILogs(context.Background(), &out, "http://192.0.2.1:8471", false); err == nil {
		t.Fatal("non-loopback address must be rejected")
	}
	if out.Len() != 0 {
		t.Errorf("nothing should be written before validation: %q", out.String())
	}
}

func TestRunAPILogs_UnauthorizedAndSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[{"timestamp":"t","level":"INFO","message":"hello"}]`))
	}))
	defer srv.Close()

	t.Setenv("DSO_AUTH_TOKEN", "")
	var out bytes.Buffer
	if err := runAPILogs(context.Background(), &out, srv.URL, false); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401 error, got %v", err)
	}

	t.Setenv("DSO_AUTH_TOKEN", "synthetic-token-0123456789")
	out.Reset()
	if err := runAPILogs(context.Background(), &out, srv.URL, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("event not printed: %q", out.String())
	}
	if strings.Contains(out.String(), "synthetic-token") {
		t.Error("token leaked into output")
	}
}

func TestRunAPILogs_FollowStopsCleanlyOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- runAPILogs(ctx, &out, srv.URL, true) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation is a normal stop, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("follow loop did not stop on cancel")
	}
}

func TestLogs_NoColorStripsAllOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"level":"ERROR","message":"boom","secret":"db"}]`))
	}))
	defer srv.Close()
	var out bytes.Buffer
	if err := runAPILogs(context.Background(), &out, srv.URL, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("ANSI escapes with NO_COLOR set: %q", out.String())
	}
	var jw bytes.Buffer
	w := &logColorWriter{writer: &jw}
	_, _ = w.Write([]byte("level=error x\n"))
	if strings.Contains(jw.String(), "\x1b[") {
		t.Error("journald writer ignored NO_COLOR")
	}
}
