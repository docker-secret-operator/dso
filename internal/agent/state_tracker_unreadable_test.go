package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// An unreadable (not unparsable) rotations.json must behave like a corrupt one: recovery
// and the operation guard stand down, and the file is never replaced. Before this was
// fixed only a JSON parse error set the corrupt flag; EACCES/EIO/EISDIR returned early
// with an EMPTY journal and no guard, and the next write renamed a fresh file over the
// one that could not be read, destroying every critical_error / in_progress record.

// seedJournal writes a real journal holding one unresolved rotation and returns its bytes.
func seedJournal(t *testing.T, dir string) []byte {
	t.Helper()
	st, err := NewStateTracker(dir, zaptest.NewLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "rot-keep", ServiceID: "svc", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal,
		OriginalIDs: []string{"orig-1"},
	}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if err != nil || len(b) == 0 {
		t.Fatalf("seed journal not written: %v", err)
	}
	return b
}

func TestStateTracker_UnreadableJournal_IsTreatedAsCorrupt(t *testing.T) {
	t.Run("a directory where the journal should be", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "rotations.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		st, err := NewStateTracker(dir, zaptest.NewLogger(t))
		if err != nil {
			t.Fatal(err)
		}
		if corrupt, where := st.Corrupt(); !corrupt || where == "" {
			t.Fatalf("an unreadable journal must set the corrupt flag (corrupt=%v where=%q)", corrupt, where)
		}
		if why, blocked := newJournalGuard(st).RotationBlocked("svc"); !blocked || why == "" {
			t.Fatal("the operation guard must block while the journal is unreadable")
		}
	})

	t.Run("a valid journal the process cannot read is preserved byte for byte", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		dir := t.TempDir()
		want := seedJournal(t, dir)
		path := filepath.Join(dir, "rotations.json")
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

		st, err := NewStateTracker(dir, zaptest.NewLogger(t))
		if err != nil {
			t.Fatal(err)
		}
		if corrupt, _ := st.Corrupt(); !corrupt {
			t.Fatal("an unreadable journal must set the corrupt flag; the tracker started with an empty journal and no guard")
		}
		if _, blocked := newJournalGuard(st).RotationBlocked("svc"); !blocked {
			t.Fatal("the operation guard must block rotation while the journal is unreadable")
		}
		// A journal write must fail closed (the caller then does not perform the action)
		// and must not replace the unread file.
		err = st.Record(context.Background(), rotation.JournalEvent{
			RotationID: "rot-new", ServiceID: "svc", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal,
		})
		if err == nil {
			t.Fatal("a journal write over an unreadable journal must fail closed")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("the unreadable journal was modified (read err %v): the evidence of the unresolved rotation is lost", err)
		}
	})
}

// Unchanged behaviour: a missing journal is a clean start, and an unparsable one is still
// quarantined with the corrupt flag.
func TestStateTracker_MissingAndParseCorruptJournalUnchanged(t *testing.T) {
	st, err := NewStateTracker(t.TempDir(), zaptest.NewLogger(t))
	if err != nil {
		t.Fatal(err)
	}
	if corrupt, _ := st.Corrupt(); corrupt {
		t.Fatal("a missing journal is a clean start, not corruption")
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "rot-1", ServiceID: "svc", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal,
	}); err != nil {
		t.Fatalf("a clean tracker must journal: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rotations.json"), []byte("\x00garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	st2, _ := NewStateTracker(dir, zaptest.NewLogger(t))
	if corrupt, where := st2.Corrupt(); !corrupt || where == "" {
		t.Fatal("a parse-corrupt journal must still be quarantined and flagged")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "rotations.json.corrupt-*")); len(matches) != 1 {
		t.Fatalf("the corrupt journal was not preserved: %v", matches)
	}
}
