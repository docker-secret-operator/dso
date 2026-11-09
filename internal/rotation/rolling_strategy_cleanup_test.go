package rotation

// The rolling strategy's rollback of a not-yet-promoted replacement ("shadow")
// must still run when the rotation context has ended, and must say how to
// recover when the cleanup itself fails.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type shadowDaemon struct {
	mu   sync.Mutex
	reqs []string
}

func (d *shadowDaemon) count(method, sub string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, r := range d.reqs {
		if strings.HasPrefix(r, method+" ") && strings.Contains(r, sub) {
			n++
		}
	}
	return n
}

// newShadowRig starts a mock daemon whose start of the replacement ends the
// rotation context and fails; deleteStatus is the answer to container removal.
func newShadowRig(t *testing.T, cancel context.CancelFunc, deleteStatus int) (*RollingStrategy, *shadowDaemon, *observer.ObservedLogs) {
	t.Helper()
	d := &shadowDaemon{}
	inspect := getBaseInspect()
	inspectJSON, _ := json.Marshal(inspect)
	createJSON, _ := json.Marshal(container.CreateResponse{ID: "new-container-id"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		isStart := strings.HasSuffix(p, "/containers/new-container-id/start")
		if !isStart && !strings.HasSuffix(p, "/containers/create") {
			time.Sleep(15 * time.Millisecond)
			if r.Context().Err() != nil { // client already abandoned this call
				return
			}
		}
		d.mu.Lock()
		d.reqs = append(d.reqs, r.Method+" "+p)
		d.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/containers/cid/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(inspectJSON)
		case strings.HasSuffix(p, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(createJSON)
		case isStart:
			cancel() // the rotation context ends while starting the replacement
			http.Error(w, `{"message":"start failed"}`, http.StatusInternalServerError)
		case r.Method == http.MethodDelete:
			if deleteStatus != http.StatusNoContent {
				http.Error(w, `{"message":"remove failed"}`, deleteStatus)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	core, logs := observer.New(zapcore.DebugLevel)
	return NewRollingStrategyWithLogger(cli, zap.New(core)), d, logs
}

func TestRollingStrategy_ContextEndsDuringStart_ShadowStillRemoved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rs, d, _ := newShadowRig(t, cancel, http.StatusNoContent)

	if err := rs.Execute(ctx, "cid", map[string]string{"K": "v"}, nil, nil, time.Second); err == nil {
		t.Fatal("expected an error")
	}
	if d.count("DELETE", "/containers/new-container-id") == 0 {
		t.Error("shadow container was not removed after the rotation context ended")
	}
	if d.count("POST", "/containers/cid/rename") != 0 || d.count("DELETE", "/containers/cid") != 0 {
		t.Error("the original container must not be renamed or removed before the replacement is healthy")
	}
}

func TestRollingStrategy_ShadowCleanupFailure_LogsRecoveryInstruction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rs, _, logs := newShadowRig(t, cancel, http.StatusInternalServerError)

	_ = rs.Execute(ctx, "cid", map[string]string{"K": "v"}, nil, nil, time.Second)

	found := false
	for _, e := range logs.FilterLevelExact(zapcore.ErrorLevel).All() {
		if strings.Contains(e.Message, "docker rm -f new-container-id") && strings.Contains(e.Message, "original container was not modified") {
			found = true
		}
	}
	if !found {
		t.Error("expected an error log with the recovery command `docker rm -f new-container-id`")
	}
}
