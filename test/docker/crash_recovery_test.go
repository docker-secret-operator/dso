//go:build dockerint

package docker

// Real-process crash-recovery harness (M0-04 / M0-11).
//
// What this does that the in-process tests cannot: it builds the dsofault-tagged
// `dso` binary, runs the REAL `dso agent` as a child process in its own process
// group (real provider plugin, real proxy, real Docker events, real state
// directory), arms it to SIGKILL ITSELF at an exact transition point, starts a
// fresh agent on the same state directory and checks what recovery did to a real
// daemon. The traffic generator, the Vault stub and the supervisor live in the test
// process and survive the agent's death.
//
// Architecture fact the harness reproduces rather than hides: the DSO proxy
// (internal/proxy) is EMBEDDED in the agent process (internal/cli/agent.go). Killing
// the agent kills the proxy listener. The reports therefore keep three things
// apart: (a) availability during an ordinary rotation, (b) availability during an
// unexpected agent death (interrupted by design; the window is reported), (c) time
// to recover after the restart.
//
// Select a slice of the matrix with DSO_CRASH_CASES=<regexp over case IDs>, e.g.
// DSO_CRASH_CASES='^rolling/' ; write the report with DSO_CRASH_REPORT=<path>.
//
// Status: compiled with -tags dockerint; NOT executed (no isolated daemon yet).

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/test/dockerguard"
	"github.com/docker-secret-operator/dso/test/harness"
)

const topologyStatement = "the DSO proxy listener is embedded in the agent process (internal/cli/agent.go: proxy.Manager is created in `dso agent`); there is no separate proxy process or container, so killing the agent kills the listener"

func TestCrash(t *testing.T) {
	sess := dockerguard.Current(t)
	arts := buildCrashArtifacts(t, sess, t.TempDir())

	t.Run("Control_OrdinaryRotation", func(t *testing.T) { testControlOrdinaryRotation(t, sess, arts) })
	t.Run("Topology_AgentDeathKillsProxyListener", func(t *testing.T) { testTopologyAgentDeath(t, sess, arts) })
	t.Run("CorruptJournal_Conservative", func(t *testing.T) { testCorruptJournal(t, sess, arts) })

	cases, err := harness.FilterCases(harness.Matrix(), os.Getenv("DSO_CRASH_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	rep := harness.Report{Started: time.Now(), Topology: topologyStatement}
	t.Run("Matrix", func(t *testing.T) {
		for _, cs := range cases {
			cs := cs
			t.Run(cs.ID(), func(t *testing.T) {
				cr := runMatrixCase(t, sess, arts, cs)
				rep.Cases = append(rep.Cases, cr)
				if cr.NotExecuted != "" {
					t.Errorf("point was not executed (an unreached point is not a pass): %s", cr.NotExecuted)
				}
			})
		}
	})
	t.Log("\n" + rep.Markdown())
	if path := os.Getenv("DSO_CRASH_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(rep.Markdown()), 0o600); err != nil {
			t.Errorf("write report: %v", err)
		}
		if js, err := rep.JSON(); err == nil {
			_ = os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".json", js, 0o600)
		}
	}
}

// runMatrixCase executes one (strategy, point) cell end to end.
func runMatrixCase(t *testing.T, sess *dockerguard.Session, arts crashArtifacts, cs harness.Case) harness.CaseReport {
	t.Helper()
	began := time.Now()
	p := cs.Point
	cr := harness.CaseReport{ID: cs.ID(), Strategy: cs.Strategy, Point: p.Name, Scenario: p.Scenario}
	defer func() { cr.Duration = time.Since(began) }()

	c := newCrashCase(t, sess, arts, cs.Strategy, p.Scenario)
	if p.Note != "" {
		cr.Notes = append(cr.Notes, p.Note)
	}

	// Stage 1: the crash. For recovery-crash cases stage 1 dies at the BASE point so
	// there is something for recovery to act on; stage 2 then dies DURING recovery.
	stage1, nth1 := p.Name, p.Nth
	if p.Scenario == harness.ScenarioCrashDuringRecovery {
		stage1, nth1 = p.Base, 1
	}
	a1 := c.startAgent(stage1, nth1)
	c.waitReady(a1)
	var marks harness.Marks
	if c.proxied() {
		c.waitProxyServing()
		c.generator = harness.StartTraffic(c.proxyAddr(), 8)
		time.Sleep(2 * time.Second) // baseline traffic before the trigger
	}
	marks.TrafficStart, marks.RotationStart = time.Now(), time.Now()
	c.arm()

	death, verified, notReached := c.awaitFault(a1, stage1)
	if notReached != "" {
		cr.NotExecuted = notReached
		return cr
	}
	marks.AgentDeath = death
	info, _ := a1.proc.Exit()
	cr.FaultFired, cr.FaultPID, cr.AgentExit, cr.PIDVerified = true, a1.proc.PID(), info.String(), verified

	crash := c.snapshot(false)
	if r, ok := crash.RecordFor(c.service); ok {
		cr.CrashJournal = r.Transition + "/" + r.Status
	}
	var ev harness.SignalEvidence
	if c.strategy == harness.Signal {
		ev.FilesAtCrash, ev.ReloadsAtCrash = c.signalEvidence()
	}
	time.Sleep(3 * time.Second) // keep the death window observable to the traffic generator

	// Stage 2: restart on the SAME state directory. The secret is made absent first so
	// the new agent cannot start a new rotation while recovery is observed.
	c.disarm()
	marks.RestartBegin = time.Now()
	var last *crashAgent
	if p.Scenario == harness.ScenarioCrashDuringRecovery {
		a2 := c.startAgent(p.Name, p.Nth)
		if _, v2, nr := c.awaitFault(a2, p.Name); nr != "" {
			cr.NotExecuted = nr
			return cr
		} else if !v2 {
			cr.PIDVerified = false
		}
		cr.Notes = append(cr.Notes, "stage 2 agent was killed DURING recovery at "+p.Name+"; a third agent completed recovery")
		last = c.startAgent("", 0)
	} else {
		last = c.startAgent("", 0)
	}
	marks.AgentReady = c.waitReady(last)
	time.Sleep(6 * time.Second) // let proxy re-registration / event handling settle while traffic runs
	final := c.snapshot(true)
	if r, ok := final.RecordFor(c.service); ok {
		cr.FinalStatus = r.Status
		cr.FinalOutcome = r.Transition
	}
	marks.TrafficEnd = time.Now()
	if c.generator != nil {
		samples := c.generator.Finish()
		c.generator = nil
		av := harness.Analyze(samples, marks, time.Time{})
		cr.Availability = &av
		if !av.ListenerDiedWithAgent() {
			cr.Notes = append(cr.Notes, "no connection was refused while the agent was dead: the documented topology (embedded proxy) may no longer hold")
			t.Errorf("topology assumption violated: the death window shows no refused connections")
		}
	}
	if c.strategy == harness.Signal {
		ev.FilesAfter, ev.ReloadsAfter = c.signalEvidence()
	}

	// Stage 3: a second recovery pass over the recovered world must change nothing.
	c.stopGracefully(last)
	again := c.startAgent("", 0)
	c.waitReady(again)
	time.Sleep(2 * time.Second)
	second := c.snapshot(false)
	c.stopGracefully(again)

	// The armed point must have fired at the boundary it names (observed at the crash,
	// before recovery): a point that fires early or late would test a different window.
	vs := harness.CheckCrashBoundary(c.world(), p, crash)
	vs = append(vs, harness.CheckRecovery(c.world(), p, crash, final)...)
	vs = append(vs, harness.CheckManualRecovery(c.world(), p, crash, final)...)
	if c.strategy == harness.Signal {
		vs = append(vs, harness.CheckSignal(c.world(), crash, final, ev)...)
	}
	vs = append(vs, harness.CheckSecondPass(final, second)...)
	cr.Violations = vs

	findings := c.leakScan()
	cr.SecretFindings = len(findings)
	for _, f := range findings {
		t.Errorf("secret sentinel leaked: %s", f)
	}
	if len(vs) > 0 {
		t.Errorf("invariant violations after %s:\n%s", p.Name, harness.FormatViolations(vs))
		for _, a := range c.agents {
			t.Logf("--- agent #%d log tail ---\n%s", a.n, logTail(a.log, 25))
		}
	}
	return cr
}

// Control: an uninterrupted rolling rotation under traffic with the REAL agent. This
// is measurement (a): availability during ORDINARY rotation. It is reported, not
// compared with the crash results.
func testControlOrdinaryRotation(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
	a := c.startAgent("", 0)
	c.waitReady(a)
	c.waitProxyServing()
	c.generator = harness.StartTraffic(c.proxyAddr(), 8)
	time.Sleep(3 * time.Second)
	var m harness.Marks
	m.RotationStart = time.Now()
	c.arm()
	waitFor(t, "rotation to complete (journal status completed)", 3*time.Minute, func() bool {
		recs, _ := harness.LoadJournal(c.root.State)
		for _, r := range recs {
			if r.ServiceID == c.service && r.Status == "completed" {
				return true
			}
		}
		return false
	})
	rotationEnd := time.Now()
	time.Sleep(8 * time.Second)
	samples := c.generator.Finish()
	c.generator = nil
	av := harness.Analyze(samples, m, rotationEnd)
	t.Logf("=== (a) ORDINARY ROTATION, real agent, %d requests ===\n%s", len(samples), av.String())
	pre := harness.Summarize(samples, time.Time{}, m.RotationStart)
	if pre.Total < 50 || pre.Failed() > 0 {
		t.Fatalf("baseline traffic not clean (%d requests, %d failed %v): results are not attributable", pre.Total, pre.Failed(), pre.FailKinds)
	}
	if f := c.leakScan(); len(f) > 0 {
		t.Errorf("secret sentinel leaked: %v", f)
	}
}

// Topology: SIGKILL the agent by PID at steady state (no rotation, no fault hook).
// The workload keeps running but the proxy listener goes with the agent. This is
// measurements (b) and (c) in their purest form, and the evidence for the topology
// statement in the documentation.
func testTopologyAgentDeath(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
	a := c.startAgent("", 0)
	c.waitReady(a)
	c.waitProxyServing()
	c.generator = harness.StartTraffic(c.proxyAddr(), 8)
	time.Sleep(3 * time.Second)

	if err := a.proc.Signal(syscall.SIGKILL); err != nil { // identity-verified, only the PID we started
		t.Fatal(err)
	}
	info, err := a.proc.WaitExit(30 * time.Second)
	if err != nil || !info.Signaled || info.Signal != syscall.SIGKILL {
		t.Fatalf("agent did not die by SIGKILL: %+v %v", info, err)
	}
	var m harness.Marks
	m.AgentDeath = time.Now()
	_ = a.proc.KillStragglers()
	time.Sleep(4 * time.Second)

	// The WORKLOAD is independent of the agent and still healthy...
	if out, err := c.dockerTry("inspect", "-f", "{{.State.Running}} {{.State.Health.Status}}", c.origID); err != nil || out != "true healthy" {
		t.Errorf("workload after agent death: %q %v", out, err)
	}
	// ...but nothing answers on the proxy port: the listener died with the agent.
	if s := harness.OneRequest(c.proxyAddr()); s.OK {
		t.Errorf("the proxy port still answers after the agent was killed: the topology statement is wrong")
	}

	m.RestartBegin = time.Now()
	b := c.startAgent("", 0)
	c.waitReady(b)
	time.Sleep(8 * time.Second)
	m.TrafficEnd = time.Now()
	samples := c.generator.Finish()
	c.generator = nil
	av := harness.Analyze(samples, m, time.Time{})
	t.Logf("=== (b)+(c) UNEXPECTED AGENT DEATH (SIGKILL, no rotation in flight) ===\n%s", av.String())
	if !av.ListenerDiedWithAgent() {
		t.Errorf("no refused connection while the agent was dead: the embedded-proxy topology no longer holds")
	}
	if !av.ServedAfterRestart {
		t.Errorf("service did not resume after the agent restarted: %s", av.String())
	}
}

// Corrupted journal: recovery must be conservative: preserve both the original and
// the candidate, preserve the unreadable file, and touch nothing.
func testCorruptJournal(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
	const point = "rolling:after_candidate_verified" // original + healthy candidate both exist
	a := c.startAgent(point, 1)
	c.waitReady(a)
	c.waitProxyServing()
	c.arm()
	if _, _, nr := c.awaitFault(a, point); nr != "" {
		t.Fatal(nr)
	}
	c.disarm()
	before := c.snapshot(false)
	if _, err := harness.CorruptJournal(c.root.State); err != nil {
		t.Fatal(err)
	}
	b := c.startAgent("", 0)
	c.waitReady(b)
	time.Sleep(4 * time.Second)
	after := c.snapshot(false)
	if !logContains(b.log, "automatic recovery is DISABLED") {
		t.Errorf("the agent did not report that recovery is disabled for an unreadable journal:\n%s", logTail(b.log, 25))
	}
	if vs := harness.CheckCorruptJournal(before, after, harness.QuarantinedJournals(c.root.State)); len(vs) > 0 {
		t.Errorf("corrupted journal handling:\n%s", harness.FormatViolations(vs))
	}
	if f := c.leakScan(); len(f) > 0 {
		t.Errorf("secret sentinel leaked: %v", f)
	}
}
