package watcher

// Rollback/cleanup must still work when the rotation context has ended
// (manual-rotation timeout, daemon shutdown): they run on a bounded cleanup
// context (rotation.CleanupContext) instead of the cancelled rotation context,
// and a rollback that itself fails is logged with recovery instructions.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type reqLog struct {
	mu sync.Mutex
	r  []string
}

func (l *reqLog) add(s string) { l.mu.Lock(); l.r = append(l.r, s); l.mu.Unlock() }

// live reports whether the client is still waiting for this request after a
// short delay. A request sent on an already-cancelled context may still reach
// the server, but the client has abandoned it (the call failed from its point
// of view), so only requests that stay live are recorded as having happened.
func live(r *http.Request) bool {
	time.Sleep(15 * time.Millisecond)
	return r.Context().Err() == nil
}
func (l *reqLog) n(method, sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := 0
	for _, e := range l.r {
		if strings.HasPrefix(e, method+" ") && strings.Contains(e, sub) {
			c++
		}
	}
	return c
}

// nEnds counts requests with the method whose request line ends with suffix.
func (l *reqLog) nEnds(method, suffix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := 0
	for _, e := range l.r {
		if strings.HasPrefix(e, method+" ") && strings.HasSuffix(e, suffix) {
			c++
		}
	}
	return c
}

func observed(rc *ReloaderController) *observer.ObservedLogs {
	core, logs := observer.New(zapcore.DebugLevel)
	rc.Logger = zap.New(core)
	return logs
}

func logsContain(logs *observer.ObservedLogs, level zapcore.Level, sub string) bool {
	for _, e := range logs.FilterLevelExact(level).All() {
		if strings.Contains(e.Message, sub) {
			return true
		}
	}
	return false
}

// A cancelled rotation context must not prevent executeRollback from removing
// the replacement, restoring the original name and restarting the original.
func TestExecuteRollback_CancelledContextStillRollsBack(t *testing.T) {
	old := rollbackRetryUnit
	rollbackRetryUnit = time.Millisecond
	t.Cleanup(func() { rollbackRetryUnit = old })
	var log reqLog
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !live(r) {
			return
		}
		log.add(r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery)
		w.WriteHeader(http.StatusNoContent)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // rotation context already over (timeout / shutdown)

	rc.executeRollback(ctx, "new-1", "orig-1", "myapp", "svc")

	if log.n("DELETE", "/containers/new-1") == 0 {
		t.Error("replacement was not removed")
	}
	if log.n("POST", "/containers/orig-1/rename") == 0 || log.n("POST", "/containers/orig-1/start") == 0 {
		t.Errorf("original not restored/restarted: %v", log.r)
	}
	if log.n("DELETE", "/containers/orig-1") != 0 {
		t.Error("the original container must never be removed by a rollback")
	}
}

// When rollback itself fails, the operator gets an error with the exact
// recovery commands and the service is marked degraded.
func TestExecuteRollback_CleanupFailureLogsRecoveryInstructions(t *testing.T) {
	old := rollbackRetryUnit
	rollbackRetryUnit = time.Millisecond
	t.Cleanup(func() { rollbackRetryUnit = old })

	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"daemon unavailable"}`, http.StatusInternalServerError)
	}))
	logs := observed(rc)

	rc.executeRollback(context.Background(), "new-1", "orig-1", "myapp", "svc")

	if _, degraded := rc.IsDegraded("svc"); !degraded {
		t.Error("service must be marked degraded when rollback fails")
	}
	for _, want := range []string{"docker rm -f new-1", "docker rename orig-1 myapp", "docker start orig-1"} {
		if !logsContain(logs, zapcore.ErrorLevel, want) {
			t.Errorf("error log lacks recovery instruction %q", want)
		}
	}
}

func TestRestoreOriginalName_FailureLogsRecoveryInstructions(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusInternalServerError)
	}))
	logs := observed(rc)
	rc.restoreOriginalName(context.Background(), "orig-1", "myapp")
	if !logsContain(logs, zapcore.ErrorLevel, "docker rename orig-1 myapp") {
		t.Error("expected an error log with the docker rename recovery command")
	}
}

func simpleRestartMock(t *testing.T, log *reqLog, onCreate, onStart func(w http.ResponseWriter)) *ReloaderController {
	t.Helper()
	inspectJSON, _ := json.Marshal(minimalInspect("myapp", "orig-1"))
	return newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.Contains(p, "/containers/create") && !strings.Contains(p, "/new-1/start") && !live(r) {
			return
		}
		log.add(r.Method + " " + p + "?" + r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet && strings.Contains(p, "/containers/orig-1/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(inspectJSON)
		case strings.Contains(p, "/containers/create"):
			onCreate(w)
		case strings.Contains(p, "/containers/new-1/start"):
			onStart(w)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}

func created(w http.ResponseWriter) {
	b, _ := json.Marshal(container.CreateResponse{ID: "new-1"})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(b)
}

// The context ends while the replacement is being created: the original must
// get its name back (rename to the original name, sent after the temp rename).
func TestExecuteSimpleRestart_ContextEndsDuringCreate_RestoresName(t *testing.T) {
	var log reqLog
	ctx, cancel := context.WithCancel(context.Background())
	rc := simpleRestartMock(t, &log,
		func(w http.ResponseWriter) {
			cancel()
			http.Error(w, `{"message":"x"}`, http.StatusInternalServerError)
		},
		func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })

	if err := rc.executeSimpleRestart(ctx, "orig-1", nil, nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if log.nEnds("POST", "/containers/orig-1/rename?name=myapp") < 1 {
		t.Errorf("original name was not restored: %v", log.r)
	}
	if log.n("DELETE", "/containers/orig-1") != 0 {
		t.Error("original must not be removed")
	}
}

// The context ends while starting the replacement: replacement removed,
// original renamed back and restarted, original never deleted.
func TestExecuteSimpleRestart_ContextEndsDuringStart_RollsBack(t *testing.T) {
	var log reqLog
	ctx, cancel := context.WithCancel(context.Background())
	rc := simpleRestartMock(t, &log, created,
		func(w http.ResponseWriter) {
			cancel()
			http.Error(w, `{"message":"x"}`, http.StatusInternalServerError)
		})

	if err := rc.executeSimpleRestart(ctx, "orig-1", nil, nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if log.n("DELETE", "/containers/new-1") == 0 {
		t.Error("replacement not removed")
	}
	if log.nEnds("POST", "/containers/orig-1/rename?name=myapp") == 0 {
		t.Errorf("original name not restored: %v", log.r)
	}
	if log.n("POST", "/containers/orig-1/start") == 0 {
		t.Error("original not restarted")
	}
	if log.n("DELETE", "/containers/orig-1") != 0 {
		t.Error("original must not be removed when the replacement did not start")
	}
}
