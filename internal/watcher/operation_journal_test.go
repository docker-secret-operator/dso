package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

type recJournal struct {
	mu     sync.Mutex
	events []rotation.JournalEvent
	failOn string
}

func (j *recJournal) Record(_ context.Context, e rotation.JournalEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e.Transition == j.failOn {
		return errors.New("disk full")
	}
	j.events = append(j.events, e)
	return nil
}

func (j *recJournal) names() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var n []string
	for _, e := range j.events {
		n = append(n, e.Transition)
	}
	return n
}

type sigCrash struct{ point string }

// runSignalUntil executes a signal rotation and "kills the process" at point.
func runSignalUntil(t *testing.T, j *recJournal, point string, ops signalOps, specs ...string) (crashed bool, err error) {
	t.Helper()
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	if len(specs) > 0 {
		r = newSignalController(t, fileSecret("a", "/run/secrets/a"), fileSecret("b", "/run/secrets/b"))
	}
	r.Journal = j
	r.RequireJournal = true
	r.FaultHook = func(p string) {
		if p == point {
			panic(sigCrash{p})
		}
	}
	defer func() {
		if rec := recover(); rec != nil {
			if _, ok := rec.(sigCrash); ok {
				crashed = true
				return
			}
			panic(rec)
		}
	}()
	tc := &TargetContainer{ID: "c1", Secrets: []string{"db"}}
	if len(specs) > 0 {
		tc.Secrets = []string{"a", "b"}
	}
	_, err = r.signalRotation(context.Background(), ops, tc, "svc", "db")
	return false, err
}

// The durable record at every interruption point must say exactly how far the
// operation got, and never claim more.
func TestSignalJournal_RecordsHowFarEachInterruptedOperationGot(t *testing.T) {
	cases := []struct {
		point string
		last  string
		multi bool
	}{
		{"signal:after_intent_publish", rotation.TrIntentSignalPublish, false},
		{"signal:after_spec_0_published", rotation.TrIntentSignalPublish, true}, // first file of a 2-file set
		{"signal:after_publish_before_ack", rotation.TrIntentSignalPublish, false},
		{"signal:after_intent_reload", rotation.TrIntentSignalReload, false},
		{"signal:after_signal_before_ack", rotation.TrIntentSignalReload, false},
		{"signal:during_readiness", rotation.TrSignalReloadSent, false},
		{"signal:before_final_ack", rotation.TrSignalReloadSent, false},
	}
	for _, c := range cases {
		t.Run(c.point, func(t *testing.T) {
			j := &recJournal{}
			var spec []string
			if c.multi {
				spec = []string{"x"}
			}
			crashed, err := runSignalUntil(t, j, c.point, okOps(&opLog{}), spec...)
			if !crashed {
				t.Fatalf("did not crash at %s: %v", c.point, err)
			}
			names := j.names()
			if names[len(names)-1] != c.last && !(c.point == "signal:after_publish_before_ack" && names[len(names)-1] == rotation.TrIntentSignalPublish) {
				t.Fatalf("last durable transition %q, want %q (all: %v)", names[len(names)-1], c.last, names)
			}
			for _, n := range names {
				if n == rotation.TrSignalDone {
					t.Fatalf("a crash must never leave a 'done' record: %v", names)
				}
			}
		})
	}
}

func TestSignalJournal_CompletePathEndsInDoneAndNeverClaimsAdoption(t *testing.T) {
	j := &recJournal{}
	if crashed, err := runSignalUntil(t, j, "", okOps(&opLog{})); crashed || err != nil {
		t.Fatalf("%v %v", crashed, err)
	}
	want := []string{rotation.TrIntentSignalPublish, rotation.TrSignalPublished, rotation.TrIntentSignalReload, rotation.TrSignalReloadSent, rotation.TrSignalDone}
	if strings.Join(j.names(), ",") != strings.Join(want, ",") {
		t.Fatalf("events %v", j.names())
	}
	last := j.events[len(j.events)-1]
	if last.State != string(SignalReloadRequested) || !strings.Contains(last.Detail, "NOT verified") {
		t.Fatalf("the terminal record must keep the limitation: %+v", last)
	}
	for _, e := range j.events {
		if strings.Contains(e.Detail, sigSentinel) {
			t.Fatal("a secret value reached the journal")
		}
	}
}

func TestSignalJournal_FailureRecordsManualRecoveryExceptWhenNothingWasPublished(t *testing.T) {
	// nothing published: no manual recovery
	j := &recJournal{}
	ops := okOps(&opLog{})
	ops.deliver = func(context.Context, string, rotation.FileSecretSpec) error { return errors.New("write failed") }
	_, _ = runSignalUntil(t, j, "", ops)
	last := j.events[len(j.events)-1]
	if last.Transition != rotation.TrSignalFailed || last.State != string(SignalNothingPublished) || last.ManualRecovery {
		t.Fatalf("%+v", last)
	}
	// published but signal failed: manual recovery
	j2 := &recJournal{}
	ops2 := okOps(&opLog{})
	ops2.signal = func(context.Context, string, string) error { return errors.New("gone") }
	_, _ = runSignalUntil(t, j2, "", ops2)
	last = j2.events[len(j2.events)-1]
	if last.State != string(SignalPublishedNotSignalled) || !last.ManualRecovery {
		t.Fatalf("%+v", last)
	}
}

func TestSignalJournal_FailedIntentBlocksPublicationAndSignal(t *testing.T) {
	for _, failOn := range []string{rotation.TrIntentSignalPublish, rotation.TrIntentSignalReload} {
		t.Run(failOn, func(t *testing.T) {
			j := &recJournal{failOn: failOn}
			l := &opLog{}
			_, err := runSignalUntil(t, j, "", okOps(l))
			if err == nil {
				t.Fatal("must fail")
			}
			if failOn == rotation.TrIntentSignalPublish && (l.has("deliver:") || l.has("signal:")) {
				t.Fatalf("no file may be published without a durable intent: %v", l.get())
			}
			if failOn == rotation.TrIntentSignalReload && l.has("signal:") {
				t.Fatalf("no signal may be sent without a durable intent: %v", l.get())
			}
		})
	}
}

func TestSignalJournal_RequireJournalRefusesWithoutOne(t *testing.T) {
	r := newSignalController(t, fileSecret("db", "/run/secrets/dso"))
	r.RequireJournal = true // no Journal wired
	l := &opLog{}
	_, err := r.signalRotation(context.Background(), okOps(l), &TargetContainer{ID: "c", Secrets: []string{"db"}}, "s", "db")
	if err == nil || l.has("deliver:") {
		t.Fatalf("must refuse before publishing: %v %v", err, l.get())
	}
}

// --- restart strategy: intents are journalled before each destructive step ---

type recDaemon struct {
	mu   sync.Mutex
	reqs []string
}

func (d *recDaemon) handler() http.Handler {
	in := minimalInspect("app", "c1")
	in.State = &container.State{Running: true}
	body, _ := json.Marshal(in)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodDelete || r.Method == http.MethodPut {
			d.reqs = append(d.reqs, r.Method+" "+r.URL.Path)
		}
		d.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/c1/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"new1"}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

func (d *recDaemon) has(sub string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.reqs {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestRestartJournal_FailedIntentBlocksEachDestructiveStep(t *testing.T) {
	cases := []struct {
		failOn       string
		mustNotSee   []string // requests that must not have reached the daemon
		mustSee      []string
		originalKept bool
	}{
		{rotation.TrIntentRenameOriginal, []string{"/rename", "/containers/create", "/stop", "/start"}, nil, true},
		{rotation.TrIntentCreateCandidate, []string{"/containers/create", "/stop", "/start"}, []string{"/rename"}, true}, // renamed away then restored
		{rotation.TrIntentStopOriginal, []string{"/containers/c1/stop", "/start"}, []string{"DELETE /v1.24/containers/new1"}, true},
	}
	for _, c := range cases {
		t.Run(c.failOn, func(t *testing.T) {
			d := &recDaemon{}
			r := newMockController(t, d.handler())
			r.Journal = &recJournal{failOn: c.failOn}
			err := r.executeSimpleRestart(context.Background(), "c1", nil, nil, nil, 0, "app")
			if err == nil || !strings.Contains(err.Error(), "journal") {
				t.Fatalf("want a journal refusal, got %v", err)
			}
			for _, bad := range c.mustNotSee {
				if d.has(bad) {
					t.Fatalf("%q reached the daemon despite a failed %s intent: %v", bad, c.failOn, d.reqs)
				}
			}
			for _, good := range c.mustSee {
				if !d.has(good) {
					t.Fatalf("expected %q (undoing the safe steps) in %v", good, d.reqs)
				}
			}
		})
	}
}
