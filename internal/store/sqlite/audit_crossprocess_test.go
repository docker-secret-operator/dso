package sqlite

// Cross-process audit-append test (verification item from the per-operation
// audit draft, section 2). An in-process test (TestAppendAuditEvent_
// ConcurrentStandaloneAppendsStayLinked) cannot show that independent OS
// processes, each with its own SQLite handle, also serialize their
// read-latest-hash-then-insert sequences. This test re-execs the test binary
// as several real processes that append to one database and then checks that
// every event is present exactly once and that the whole chain is linked
// with no forks and no gaps.
//
// Synchronization is by files in a directory this test creates: each helper
// opens the database, writes "ready-<n>", and waits for "go" before it starts
// appending, so all helpers contend at the same moment. Every wait is
// bounded.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/store"
)

const (
	auditHelperProcs  = 4
	auditHelperEvents = 25
)

// TestHelperProcessAuditAppender is not a real test: it only runs when
// DSO_TEST_AUDIT_DB is set (by TestAuditChain_MultiProcessAppendsStayLinked)
// and otherwise skips.
func TestHelperProcessAuditAppender(t *testing.T) {
	path := os.Getenv("DSO_TEST_AUDIT_DB")
	if path == "" {
		t.Skip("helper process")
	}
	idx := os.Getenv("DSO_TEST_AUDIT_IDX")
	syncDir := os.Getenv("DSO_TEST_AUDIT_SYNC")
	n, _ := strconv.Atoi(os.Getenv("DSO_TEST_AUDIT_N"))

	s, err := Open(ctx, path, Options{BusyTimeout: 30 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit helper: open:", err)
		os.Exit(2)
	}
	defer s.Close()
	if err := os.WriteFile(filepath.Join(syncDir, "ready-"+idx), []byte("ready"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "audit helper: signalling ready:", err)
		os.Exit(2)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(syncDir, "go")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "audit helper: timed out waiting for go")
			os.Exit(2)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for j := 0; j < n; j++ {
		id := fmt.Sprintf("p%s-e%03d", idx, j)
		if _, err := s.AppendAuditEvent(ctx, store.AuditEvent{
			ID: id, EventType: store.AuditProjectCreate, Outcome: store.AuditSuccess,
			ProjectSlug: "proj", SecretName: id,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "audit helper: append:", err)
			os.Exit(3)
		}
	}
}

func TestAuditChain_MultiProcessAppendsStayLinked(t *testing.T) {
	s, path := open(t)

	// A few baseline events so the helpers append to an existing chain, not
	// to an empty one (the empty-chain first append is a separate case).
	const baseline = 3
	for i := 0; i < baseline; i++ {
		id := fmt.Sprintf("base-%d", i)
		if _, err := s.AppendAuditEvent(ctx, ev(id, store.AuditInit)); err != nil {
			t.Fatal(err)
		}
	}

	syncDir := t.TempDir() // removed by the testing package; holds only this test's sync files
	runCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmds := make([]*exec.Cmd, auditHelperProcs)
	for i := range cmds {
		c := exec.CommandContext(runCtx, os.Args[0], "-test.run=^TestHelperProcessAuditAppender$") // #nosec G204 -- os.Args[0] is this test binary itself
		c.Env = append(os.Environ(),
			"DSO_TEST_AUDIT_DB="+path,
			"DSO_TEST_AUDIT_IDX="+strconv.Itoa(i),
			"DSO_TEST_AUDIT_N="+strconv.Itoa(auditHelperEvents),
			"DSO_TEST_AUDIT_SYNC="+syncDir)
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = c
	}
	// If anything below fails early, do not leave helpers running.
	t.Cleanup(func() {
		for _, c := range cmds {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		}
	})

	// Barrier: wait (bounded) until every helper has opened the database.
	deadline := time.Now().Add(60 * time.Second)
	for i := range cmds {
		ready := filepath.Join(syncDir, "ready-"+strconv.Itoa(i))
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("helper %d never became ready", i)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if err := os.WriteFile(filepath.Join(syncDir, "go"), []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("helper %d failed: %v", i, err)
		}
	}

	events, err := s.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := baseline + auditHelperProcs*auditHelperEvents
	if len(events) != want {
		t.Fatalf("event count = %d, want %d (lost or duplicated appends)", len(events), want)
	}

	// Every expected event is present exactly once, and each helper's own
	// events appear in the order it appended them.
	seen := make(map[string]int, len(events))
	lastIdx := make(map[int]int) // helper -> last event index seen
	for pos, e := range events {
		if prev, dup := seen[e.ID]; dup {
			t.Fatalf("event %q appears twice (positions %d and %d)", e.ID, prev, pos)
		}
		seen[e.ID] = pos
		var p, j int
		if n, _ := fmt.Sscanf(e.ID, "p%d-e%d", &p, &j); n == 2 {
			if last, ok := lastIdx[p]; ok && j != last+1 {
				t.Fatalf("helper %d events out of order: %d after %d", p, j, last)
			}
			if _, ok := lastIdx[p]; !ok && j != 0 {
				t.Fatalf("helper %d first event is %d, want 0", p, j)
			}
			lastIdx[p] = j
		}
	}
	for p := 0; p < auditHelperProcs; p++ {
		if lastIdx[p] != auditHelperEvents-1 {
			t.Fatalf("helper %d: last event index %d, want %d", p, lastIdx[p], auditHelperEvents-1)
		}
	}
	for i := 0; i < baseline; i++ {
		if _, ok := seen[fmt.Sprintf("base-%d", i)]; !ok {
			t.Fatalf("baseline event base-%d missing", i)
		}
	}

	// No fork: no two events share a prev_hash, and each links to the one
	// before it. Then the full chain verifier.
	prevSeen := make(map[string]int)
	for pos, e := range events {
		key := string(e.PrevHash)
		if other, dup := prevSeen[key]; dup {
			t.Fatalf("chain forked: events at positions %d and %d share a prev_hash", other, pos)
		}
		prevSeen[key] = pos
		if pos > 0 && string(e.PrevHash) != string(events[pos-1].EventHash) {
			t.Fatalf("event %d does not link to event %d", pos, pos-1)
		}
	}
	if err := VerifyAuditChain(events); err != nil {
		t.Fatalf("chain verification failed after multi-process appends: %v", err)
	}
}
