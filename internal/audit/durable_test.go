package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSink(t *testing.T) (*DurableSink, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state", "audit.jsonl")
	d, err := OpenDurable(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, p
}

func TestDurable_AppendVerifyAndReopenContinuesChain(t *testing.T) {
	d, p := newSink(t)
	for _, e := range []string{"a", "b", "c"} {
		if err := d.Append(e, "op", "", "sec", "", "success"); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Close()
	d2, err := OpenDurable(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if err := d2.Append("d", "op", "", "", "", "success"); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadVerified(p)
	if err != nil || len(recs) != 4 || recs[3].Seq != 4 {
		t.Fatalf("recs=%d err=%v", len(recs), err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
}

func TestDurable_TamperingDetected(t *testing.T) {
	d, p := newSink(t)
	for i := 0; i < 3; i++ {
		_ = d.Append("evt", "operator", "", "s", "", "success")
	}
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), `"user":"operator"`, `"user":"mallory"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVerified(p); !errors.Is(err, ErrChain) {
		t.Fatalf("edit not detected: %v", err)
	}
	if _, err := OpenDurable(p); !errors.Is(err, ErrChain) {
		t.Fatalf("open must refuse a broken chain: %v", err)
	}
}

func TestDurable_RemovedMiddleRecordDetected(t *testing.T) {
	d, p := newSink(t)
	for i := 0; i < 3; i++ {
		_ = d.Append("evt", "op", "", "s", "", "success")
	}
	lines := strings.SplitAfter(string(mustRead(t, p)), "\n")
	if err := os.WriteFile(p, []byte(lines[0]+lines[2]), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVerified(p); !errors.Is(err, ErrChain) {
		t.Fatalf("gap not detected: %v", err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDurable_TornTailDiscardedAndAppendContinues(t *testing.T) {
	d, p := newSink(t)
	_ = d.Append("one", "op", "", "", "", "success")
	_ = d.Close()
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"seq":2,"time":"x","ev`)
	_ = f.Close()
	d2, err := OpenDurable(p)
	if err != nil {
		t.Fatalf("torn tail must be tolerated: %v", err)
	}
	defer d2.Close()
	if err := d2.Append("two", "op", "", "", "", "success"); err != nil {
		t.Fatal(err)
	}
	if recs, err := ReadVerified(p); err != nil || len(recs) != 2 {
		t.Fatalf("recs=%d err=%v", len(recs), err)
	}
}

func TestDurable_RefusesWorldReadableFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDurable(p); err == nil {
		t.Fatal("expected refusal for mode 0644")
	}
}

func TestLog_WritesEveryEventToDurableSink(t *testing.T) {
	d, p := newSink(t)
	SetDurable(d)
	defer SetDurable(nil)
	Log(context.Background(), "manual_rotation_triggered", "token:ab12", "", "db-password", "", "success")
	Log(context.Background(), "auth_login", "operator", "", "", "", "failure")
	recs, err := ReadVerified(p)
	if err != nil || len(recs) != 2 || recs[0].Event != "manual_rotation_triggered" || recs[1].Status != "failure" {
		t.Fatalf("recs=%+v err=%v", recs, err)
	}
	if DurablePath() != p {
		t.Fatal("DurablePath mismatch")
	}
}
