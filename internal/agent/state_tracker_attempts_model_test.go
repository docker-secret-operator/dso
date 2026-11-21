package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Batch A / step S2-narrow (A-02, A-13, A-14 data-model parts only): the
// RotationState data model gains Attempts, JournalSchema and AttemptState. Nothing in
// the tracker reads or writes them yet (S3 does), so these tests pin that the model is
// inert: it deep-copies, round-trips, leaves v1 journals byte-identical, degrades
// cleanly through an N-1-shaped reader, and does not weaken S1's staging guarantee.
//
// Docker-free; self-contained (no helper from another test file).

func attemptsFixture() []AttemptState {
	return []AttemptState{
		{
			Attempt: 1, Strategy: "rolling", TargetOriginalID: "orig-1", ServiceName: "web",
			State: "rolled_back", BeganSeq: 2, EndedSeq: 5, Detail: "d1",
		},
		{
			Attempt: 2, ParentAttempt: 1, Strategy: "restart", TargetOriginalID: "orig-1", ServiceName: "web",
			State: "escalated", BeganSeq: 6, EndedSeq: 7, ManualReason: "free-text reason",
			RecoveryPass: 1, RecoveryStep: "step-a", ComposeProject: "proj", ComposeService: "web",
		},
	}
}

func attemptsTracker(t *testing.T) (*StateTracker, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	return st, dir
}

func attemptsReadJournal(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return b
}

// Deep copy: mutating a clone, including appending and editing elements, must leave
// the original untouched. S1's staging relies on clone() being complete.
func TestS2Model_CloneDeepCopiesAttempts(t *testing.T) {
	orig := &RotationState{RotationID: "r", Status: "in_progress", JournalSchema: 2, Attempts: attemptsFixture()}
	want := attemptsFixture()

	c := orig.clone()
	if !reflect.DeepEqual(c.Attempts, want) || c.JournalSchema != 2 {
		t.Fatalf("clone differs from the original: %+v", c)
	}
	c.Attempts[0].State = "mutated"
	c.Attempts[1].ManualReason = "mutated"
	c.Attempts = append(c.Attempts, AttemptState{Attempt: 3})
	c.Attempts[0].Detail = "mutated"

	if !reflect.DeepEqual(orig.Attempts, want) {
		t.Fatalf("mutating the clone changed the original: %+v", orig.Attempts)
	}

	// The backing array must not be shared even when the clone is not appended to.
	c2 := orig.clone()
	c2.Attempts[1].RecoveryStep = "x"
	if orig.Attempts[1].RecoveryStep != "step-a" {
		t.Fatal("clone shares the Attempts backing array with the original")
	}
}

func TestS2Model_CloneKeepsAbsentAttemptsAbsent(t *testing.T) {
	c := (&RotationState{RotationID: "r"}).clone()
	if c.Attempts != nil || c.JournalSchema != 0 {
		t.Fatalf("a record without attempts must clone to one without attempts, got %+v / %d", c.Attempts, c.JournalSchema)
	}
	if e := (&RotationState{RotationID: "r", Attempts: []AttemptState{}}).clone(); len(e.Attempts) != 0 {
		t.Fatalf("an empty Attempts must clone to an empty one, got %+v", e.Attempts)
	}
}

// Schema round trip: the new fields survive persist and reload through the real
// tracker, and use the agreed JSON keys.
func TestS2Model_SchemaRoundTrip(t *testing.T) {
	st, dir := attemptsTracker(t)
	if err := st.StartRotationWithID("r1", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	live := st.byID("r1")
	live.JournalSchema, live.Attempts = 2, attemptsFixture()
	err := st.persistState()
	st.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	var onDisk map[string]map[string]any
	if err := json.Unmarshal(attemptsReadJournal(t, dir), &onDisk); err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 1 {
		t.Fatalf("expected one record on disk, got %d", len(onDisk))
	}
	for _, rec := range onDisk {
		if rec["journal_schema"] != float64(2) {
			t.Fatalf("journal_schema = %v, want 2", rec["journal_schema"])
		}
		list, ok := rec["attempts"].([]any)
		if !ok || len(list) != 2 {
			t.Fatalf("attempts = %v, want a list of two", rec["attempts"])
		}
		second := list[1].(map[string]any)
		for k, want := range map[string]any{"service_name": "web", "manual_reason": "free-text reason",
			"target_original_id": "orig-1", "parent_attempt": float64(1), "began_seq": float64(6), "strategy": "restart"} {
			if second[k] != want {
				t.Fatalf("attempts[1][%q] = %v, want %v", k, second[k], want)
			}
		}
	}

	st2, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	st2.mu.Lock()
	got := st2.byID("r1")
	schema, attempts := got.JournalSchema, got.Attempts
	st2.mu.Unlock()
	if schema != 2 || !reflect.DeepEqual(attempts, attemptsFixture()) {
		t.Fatalf("reloaded record lost the model: schema=%d attempts=%+v", schema, attempts)
	}
}

// v1 compatibility: a journal that never carried the new fields has no such keys after
// a no-op load and persist, and its bytes are identical (deterministic fixture built
// by the tracker itself, not captured from a binary).
func TestS2Model_V1JournalIsByteIdenticalAfterNoOpReload(t *testing.T) {
	st, dir := attemptsTracker(t)
	ctx := context.Background()
	if err := st.StartRotationWithID("v1", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "v1", ServiceID: "svc", Transition: rotation.TrIntentCreateCandidate}); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "v1", Transition: rotation.TrCompleted}); err != nil {
		t.Fatal(err)
	}
	before := attemptsReadJournal(t, dir)
	for _, key := range []string{"journal_schema", `"attempts"`} {
		if bytes.Contains(before, []byte(key)) {
			t.Fatalf("a v1 journal must not carry %s:\n%s", key, before)
		}
	}

	st2, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	st2.mu.Lock()
	err = st2.persistState()
	st2.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if after := attemptsReadJournal(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("a no-op reload and persist changed the v1 journal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	st2.mu.Lock()
	got := st2.byID("v1")
	nAttempts, schema := len(got.Attempts), got.JournalSchema
	st2.mu.Unlock()
	if nAttempts != 0 || schema != 0 {
		t.Fatalf("a v1 record must load without attempts or schema, got %d / %d", nAttempts, schema)
	}
}

// n1RotationState is an N-1-shaped reader: it knows the record's older fields and none
// of the new ones, and (like encoding/json without DisallowUnknownFields) drops what it
// does not know when it re-encodes.
type n1RotationState struct {
	ProviderName        string `json:"provider_name"`
	SecretName          string `json:"secret_name"`
	OriginalContainerID string `json:"original_container_id"`
	NewContainerID      string `json:"new_container_id"`
	RotationID          string `json:"rotation_id,omitempty"`
	ServiceID           string `json:"service_id,omitempty"`
	Status              string `json:"status"`
	Seq                 int    `json:"seq,omitempty"`
}

// N-1 decode compatibility: a record carrying the new fields decodes through an
// N-1-shaped struct without error, its re-encoding drops the new fields, and the
// current type reads that result as a record without attempts. This models what an N-1
// writer's rewrite does; it is NOT a test against a real N-1 binary (unverified).
func TestS2Model_N1ShapedDecodeDropsNewFieldsAndCurrentTypeReadsLegacy(t *testing.T) {
	st, dir := attemptsTracker(t)
	if err := st.StartRotationWithID("r1", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	live := st.byID("r1")
	live.JournalSchema, live.Attempts = 2, attemptsFixture()
	err := st.persistState()
	st.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	var n1 map[string]*n1RotationState
	if err := json.Unmarshal(attemptsReadJournal(t, dir), &n1); err != nil {
		t.Fatalf("an N-1-shaped reader must decode a record with the new fields: %v", err)
	}
	if len(n1) != 1 {
		t.Fatalf("expected one record, got %d", len(n1))
	}
	rewritten, err := json.Marshal(n1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rewritten, []byte("attempts")) || bytes.Contains(rewritten, []byte("journal_schema")) {
		t.Fatalf("the N-1 rewrite should not carry the new fields: %s", rewritten)
	}

	var cur map[string]*RotationState
	if err := json.Unmarshal(rewritten, &cur); err != nil {
		t.Fatalf("the current type must read an N-1-rewritten record: %v", err)
	}
	for _, r := range cur {
		if r.RotationID != "r1" || len(r.Attempts) != 0 || r.JournalSchema != 0 {
			t.Fatalf("expected a legacy record without attempts, got %+v", r)
		}
	}
}

// S1 staging isolation: a staged clone never aliases the live Attempts, and a failed
// write leaves the live Attempts and the journal file unchanged.
func TestS2Model_StagingDoesNotAliasOrLeakAttempts(t *testing.T) {
	st, dir := attemptsTracker(t)
	if err := st.StartRotationWithID("r1", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	live := st.byID("r1")
	live.JournalSchema, live.Attempts = 2, attemptsFixture()
	err := st.persistState()
	st.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	fileBefore := attemptsReadJournal(t, dir)

	// A staged clone is independent of the live record.
	st.mu.Lock()
	tx := st.beginStage()
	staged := tx.touch(st.byID("r1"))
	if staged == st.byID("r1") {
		st.mu.Unlock()
		t.Fatal("touch returned the live record, not a clone")
	}
	staged.Attempts[0].State = "leak"
	staged.Attempts = append(staged.Attempts, AttemptState{Attempt: 9})
	leaked := !reflect.DeepEqual(st.byID("r1").Attempts, attemptsFixture())
	st.mu.Unlock()
	if leaked {
		t.Fatal("editing a staged clone changed the live Attempts before any commit")
	}

	// A write that fails before the rename leaves memory and file as they were.
	tmp := filepath.Join(dir, "rotations.json.tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatalf("cannot install the write fault: %v", err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCandidateCreated}); err == nil {
		t.Fatal("the write was expected to fail")
	}
	st.mu.Lock()
	after := st.byID("r1")
	same := reflect.DeepEqual(after.Attempts, attemptsFixture()) && after.JournalSchema == 2 && after.Seq == 0
	st.mu.Unlock()
	if !same {
		t.Fatal("a failed write changed the live Attempts, schema or Seq")
	}
	if !bytes.Equal(fileBefore, attemptsReadJournal(t, dir)) {
		t.Fatal("a failed write changed the journal file")
	}
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}

	// After the fault is gone the same event commits and carries the Attempts through.
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCandidateCreated}); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	got := st.byID("r1")
	ok := reflect.DeepEqual(got.Attempts, attemptsFixture()) && got.JournalSchema == 2 && got.Seq == 1
	st.mu.Unlock()
	if !ok {
		t.Fatal("a committed event must preserve the Attempts and schema of the record")
	}
}
