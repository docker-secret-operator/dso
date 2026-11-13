package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportCountsAndRendering(t *testing.T) {
	good := CaseReport{ID: "rolling/a", Strategy: Rolling, Point: "rolling:a", FaultFired: true, PIDVerified: true, FinalStatus: "recovered"}
	bad := CaseReport{ID: "rolling/b", Strategy: Rolling, Point: "rolling:b", FaultFired: true, PIDVerified: true,
		Violations: []Violation{{"workload-survives", "gone"}}}
	leak := CaseReport{ID: "signal/c", Strategy: Signal, Point: "signal:c", FaultFired: true, PIDVerified: true, SecretFindings: 1}
	unverified := CaseReport{ID: "restart/d", Strategy: Restart, Point: "restart:d", FaultFired: true, PIDVerified: false}
	skipped := CaseReport{ID: "restart/e", Strategy: Restart, Point: "restart:e", NotExecuted: "point never reached"}
	av := Analyze([]Sample{ok(0, "a"), fail(10, "connection_refused"), ok(900, "a")}, Marks{AgentDeath: at(5), RestartBegin: at(800)}, time.Time{})
	good.Availability = &av

	r := Report{Started: time.Unix(0, 0).UTC(), Topology: "proxy embedded in the agent", Cases: []CaseReport{good, bad, leak, unverified, skipped}}
	p, f, n := r.Counts()
	if p != 1 || f != 3 || n != 1 {
		t.Fatalf("counts %d/%d/%d", p, f, n)
	}
	md := r.Markdown()
	for _, want := range []string{"1 passed, 3 failed, 1 not executed", "workload-survives", "NOT EXECUTED: point never reached", "(b) unexpected agent death", "proxy embedded in the agent"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if unverified.Passed() || leak.Passed() || skipped.Passed() || bad.Passed() {
		t.Fatal("a failing case passed")
	}
	raw, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil || len(back.Cases) != 5 {
		t.Fatalf("%v", err)
	}
}

func TestNewRootLayoutAndAgentArgs(t *testing.T) {
	r, err := NewRoot(t.TempDir())
	if err != nil {
		t.Skipf("TMPDIR too long for unix sockets here: %v", err)
	}
	for _, d := range []string{r.Cfg, r.State, r.Lock, r.Logs, r.Home, r.Sock} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", d, fi, err)
		}
	}
	if err := r.WriteConfig(AgentConfig{OwnerID: "dso-crash-0123abcd", VaultAddr: "http://127.0.0.1:1", VaultToken: "t"}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(r.Cfg, "dso.yaml")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config perms: %v %v", fi, err)
	}
	args := strings.Join(r.AgentArgs("127.0.0.1:1", "127.0.0.1:2"), " ")
	for _, want := range []string{"--config dso.yaml agent", filepath.Join(r.Sock, "dso.sock"), "--api-addr 127.0.0.1:1"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 120))
	if err := os.MkdirAll(long, 0o700); err == nil {
		if _, err := NewRoot(long); err == nil {
			t.Fatal("an over-long socket path must be rejected up front")
		}
	}
}
