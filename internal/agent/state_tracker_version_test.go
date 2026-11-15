package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"go.uber.org/zap"
)

// O-2: the journal records the target secret version at StartRotationWithVersion,
// as a keyed HMAC identity in the bridge encoding, and nothing else.

func TestStartRotationWithVersion_RecordsHMACVersionDurably(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	ver := FormatJournalSecretVersion(r0ver(t))
	if ver == "" {
		t.Fatal("fixture version not representable")
	}
	if err := st.StartRotationWithVersion("rot-v1", "vault", "db", "o1", "", ver); err != nil {
		t.Fatal(err)
	}
	// A strategy event without a version never clears it.
	if err := st.Record(rotation.WithRotationID(context.Background(), "rot-v1"),
		rotation.JournalEvent{RotationID: "rot-v1", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal}); err != nil {
		t.Fatal(err)
	}
	// Durable: a new process reads it back.
	st2, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	rec := JournalRecordByID(st2, "rot-v1")
	if rec == nil || rec.SecretVersion != ver {
		t.Fatalf("record %+v, want secret_version %q", rec, ver)
	}
	if v := JournalRecordFor(rec).Version; v == nil || FormatJournalSecretVersion(*v) != ver {
		t.Fatalf("bridge view lost the version: %+v", v)
	}
	// StartRotationWithID keeps the pre-O-2 behaviour (no version).
	if err := st2.StartRotationWithID("rot-v2", "vault", "db", "o2", ""); err != nil {
		t.Fatal(err)
	}
	if rec := JournalRecordByID(st2, "rot-v2"); rec == nil || rec.SecretVersion != "" {
		t.Fatalf("StartRotationWithID must not invent a version: %+v", rec)
	}
}

func TestStartRotationWithVersion_RefusesAnythingButAKeyedHMAC(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	good := FormatJournalSecretVersion(r0ver(t))
	_, mac, _ := strings.Cut(strings.TrimPrefix(good, "hmac:"), ":")
	bad := []string{
		"hunter2",                           // a value
		"provider::v42",                     // a provider id, not keyed
		"sha256:" + strings.Repeat("a", 64), // an unkeyed hash
		"hmac::" + mac,                      // no key id
		"hmac:k-xyz:" + mac,                 // malformed key id
		"hmac:k-0123456789abcdef:short",     // not a 32-byte MAC
		"hmac:k-0123456789abcdef:" + mac + "=",
	}
	before, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	for _, b := range bad {
		err := st.StartRotationWithVersion("rot-bad", "vault", "db", "o1", "", b)
		if err == nil {
			t.Errorf("%q accepted", b)
			continue
		}
		if strings.Contains(err.Error(), b) {
			t.Errorf("the refusal echoes the input %q", b)
		}
	}
	if JournalRecordByID(st, "rot-bad") != nil {
		t.Fatal("a refused start recorded something")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("a refused start wrote the journal")
	}
}

// A journal written before O-2 (no secret_version field) still loads and reads
// as "version unknown".
func TestStartRotationWithVersion_OldJournalWithoutFieldLoads(t *testing.T) {
	dir := t.TempDir()
	old := `{"vault:db:o1":{"provider_name":"vault","secret_name":"db","original_container_id":"o1","new_container_id":"","rotation_id":"rot-old","status":"completed","start_time":"2026-11-01T00:00:00Z","last_update":"2026-11-01T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Corrupt(); c {
		t.Fatal("an old journal was treated as corrupt")
	}
	rec := JournalRecordByID(st, "rot-old")
	if rec == nil || rec.SecretVersion != "" || JournalRecordFor(rec).Version != nil {
		t.Fatalf("old record: %+v", rec)
	}
}
