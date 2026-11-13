package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// The harness reads the REAL persisted state. Produce it with the real
// StateTracker (as the agent does) and parse it with the harness types; a drift
// in a JSON tag shows up here.
func TestRotationRecordMatchesTheRealPersistedState(t *testing.T) {
	dir := t.TempDir()
	st, err := agent.NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	events := []rotation.JournalEvent{
		{RotationID: "rot-1", ServiceID: "svc", OwnerID: "o", Strategy: "rolling", Transition: rotation.TrIntentCreateCandidate, OriginalIDs: []string{"orig1"}},
		{RotationID: "rot-1", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"cand1"}},
		{RotationID: "rot-1", Transition: rotation.TrIntentHandoff, ExpectedProxyTarget: "cand1"},
		{RotationID: "rot-1", Transition: rotation.TrSwapCommitted, ObservedProxyTarget: "cand1", State: "x", ManualRecovery: true, Detail: "d"},
	}
	for _, e := range events {
		if err := st.Record(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := LoadJournal(dir)
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v %d", err, len(recs))
	}
	r := recs[0]
	if r.RotationID != "rot-1" || r.ServiceID != "svc" || r.OwnerID != "o" || r.Strategy != "rolling" ||
		r.Transition != rotation.TrSwapCommitted || !r.Committed || r.ExpectedProxyTarget != "cand1" || r.ObservedProxyTarget != "cand1" ||
		len(r.Originals()) != 1 || r.Originals()[0] != "orig1" || len(r.CandidateContainerIDs) != 1 || r.CandidateContainerIDs[0] != "cand1" ||
		r.OperationState != "x" || !r.ManualRecovery || r.Detail != "d" || r.Status != "in_progress" || r.Seq != 4 {
		t.Fatalf("parsed record does not match what the real tracker wrote: %+v", r)
	}
	// Every JSON key the real type emits is a key the harness type knows (or is
	// listed as deliberately ignored), so a renamed field cannot pass silently.
	raw, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	var generic map[string]map[string]any
	_ = json.Unmarshal(raw, &generic)
	known := map[string]bool{}
	rt := reflect.TypeOf(RotationRecord{})
	for i := 0; i < rt.NumField(); i++ {
		known[strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	for k := range generic["journal:rot-1"] {
		if !known[k] && k != "secret_version" {
			t.Errorf("the agent persists %q but the harness record does not model it", k)
		}
	}
}

func TestDigestIgnoresTimesAndOrder(t *testing.T) {
	a := []RotationRecord{{RotationID: "a", Status: "recovered", LastUpdate: time.Now()}, {RotationID: "b", Status: "critical_error"}}
	b := []RotationRecord{{RotationID: "b", Status: "critical_error", LastUpdate: time.Now().Add(time.Hour)}, {RotationID: "a", Status: "recovered"}}
	if Digest(a) != Digest(b) {
		t.Fatal("digest depends on time or order")
	}
	b[1].Status = "completed"
	if Digest(a) == Digest(b) {
		t.Fatal("digest ignores a status change")
	}
}

func TestCorruptJournalIsPreservedByTheRealTracker(t *testing.T) {
	dir := t.TempDir()
	st, _ := agent.NewStateTracker(dir, zap.NewNop())
	for i, tr := range []string{rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated, rotation.TrCandidateReady} {
		if err := st.Record(t.Context(), rotation.JournalEvent{RotationID: "rot-9", ServiceID: "svc", Strategy: "rolling", Transition: tr, OriginalIDs: []string{"o" + string(rune('a'+i))}}); err != nil {
			t.Fatal(err)
		}
	}
	orig, err := CorruptJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := agent.NewStateTracker(dir, zap.NewNop())
	corrupt, where := st2.Corrupt()
	if !corrupt {
		t.Fatal("the real tracker did not detect the corrupted journal")
	}
	q := QuarantinedJournals(dir)
	if len(q) != 1 || q[0] != where {
		t.Fatalf("quarantined=%v where=%q", q, where)
	}
	if b, _ := os.ReadFile(q[0]); len(b) != len(orig)/2 {
		t.Fatal("the preserved evidence is not the corrupted file")
	}
}
