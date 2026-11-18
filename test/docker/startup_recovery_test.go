//go:build dockerint

package docker

// Real-Docker scenarios for M0-04 findings 3.2 (traffic eligibility of candidates at
// agent start) and 3.3 (first-poll / competing rotation dispatch over an unresolved
// operation). They reuse the real-process crash harness (crash_support_test.go): the
// real `dso agent` is killed at an exact transition point and a fresh agent is started
// on the same state directory against a real daemon.
//
// Status: COMPILED with -tags dockerint; NOT EXECUTED. No isolated daemon exists yet
// and the dev daemon is forbidden. The Docker-free regression tests that cover the
// same decisions deterministically live in internal/agent (startup_reconstruction_test.go,
// operation_guard_test.go), internal/watcher (operation_guard_test.go) and
// internal/proxy. These scenarios are what must be run on the disposable VM to confirm
// the behaviour against a real daemon, real events and a real proxy.

import (
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/test/dockerguard"
	"github.com/docker-secret-operator/dso/test/harness"
)

func TestStartupRecovery(t *testing.T) {
	sess := dockerguard.Current(t)
	arts := buildCrashArtifacts(t, sess, t.TempDir())

	t.Run("CandidateNeverRoutedBeforeRecovery", func(t *testing.T) { testCandidateNeverRouted(t, sess, arts) })
	t.Run("SignalPartialPublication_NoCompetingRotationAfterRestart", func(t *testing.T) { testNoCompetingSignalRotation(t, sess, arts) })
	t.Run("CorruptJournal_BlocksRotationAfterRestart", func(t *testing.T) { testCorruptJournalBlocksRotation(t, sess, arts) })
	t.Run("CleanRestart_SettledValueNotRotatedAgain_ChangedValueStillRotates", func(t *testing.T) { testInitialisationRotationStillRuns(t, sess, arts) })
}

// identityOf returns the identity the application serves (the body the traffic
// generator records as Sample.Backend).
func (c *crashCase) identityOf(id string) string {
	out, err := c.dockerTry("exec", id, "cat", "/www/index.html")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// 3.2. The agent dies after the rotation candidate was created and started (cloned
// labels include dso.host_ports) but before the proxy handoff. A fresh agent scans
// running containers before recovery. The candidate must NOT receive a single client
// connection from the moment the new agent's proxy listens, through recovery, until it
// is retired; only the original answers.
func testCandidateNeverRouted(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	for _, point := range []string{"rolling:after_start_before_verify", "rolling:after_candidate_verified"} {
		point := point
		t.Run(strings.TrimPrefix(point, "rolling:"), func(t *testing.T) {
			c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
			a1 := c.startAgent(point, 1)
			c.waitReady(a1)
			c.waitProxyServing()
			c.arm()
			if _, _, nr := c.awaitFault(a1, point); nr != "" {
				t.Fatal(nr)
			}
			c.disarm()

			origIdent := c.identityOf(c.origID)
			candIdent := ""
			for _, v := range c.snapshot(false).Containers {
				if v.ID != c.origID && v.Owner == c.owner && v.Running && strings.Contains(v.Name, "_dso_new_") {
					candIdent = c.identityOf(v.ID)
				}
			}
			if origIdent == "" || candIdent == "" || origIdent == candIdent {
				t.Fatalf("test precondition: need two distinguishable running backends (orig %q candidate %q)", origIdent, candIdent)
			}

			// Traffic starts BEFORE the new agent exists, so every connection the new
			// proxy ever accepts is measured from its very first accept.
			gen := harness.StartTraffic(c.proxyAddr(), 8)
			a2 := c.startAgent("", 0)
			c.waitReady(a2)
			time.Sleep(8 * time.Second) // recovery, held-candidate re-evaluation, steady state
			samples := gen.Finish()

			served := map[string]int{}
			for _, s := range samples {
				if s.OK {
					served[s.Backend]++
				}
			}
			if served[candIdent] != 0 {
				t.Errorf("the unsettled rotation candidate answered %d client connections (identities served: %v)", served[candIdent], served)
			}
			if served[origIdent] == 0 {
				t.Errorf("the original never served after the restart (identities served: %v)", served)
			}
			if !logContains(a2.log, "held back from traffic") {
				t.Errorf("the new agent did not report holding the candidate:\n%s", logTail(a2.log, 40))
			}
			c.stopGracefully(a2)
		})
	}
}

// 3.3. A signal rotation died with the first of two secret files replaced (genuinely
// partial publication). The Vault stub still serves the NEW secrets, so a fresh agent's
// first poll WOULD dispatch a rotation. It must be refused: files stay exactly as the
// crash left them, the application is not reloaded, the journal stays critical_error,
// and the refusal is reported.
func testNoCompetingSignalRotation(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Signal, harness.ScenarioHappy)
	const point = "signal:after_spec_0_published"
	a1 := c.startAgent(point, 1)
	c.waitReady(a1)
	c.arm()
	if _, _, nr := c.awaitFault(a1, point); nr != "" {
		t.Fatal(nr)
	}
	filesAtCrash, reloadsAtCrash := c.signalEvidence()

	// NOT disarmed: the new secrets stay visible to the restarted agent.
	a2 := c.startAgent("", 0)
	c.waitReady(a2)
	waitFor(t, "the first poll to be refused", 60*time.Second, func() bool {
		return logContains(a2.log, "Rotation refused: the service has an unresolved operation")
	})
	time.Sleep(10 * time.Second) // further polls must not change anything either
	filesAfter, reloadsAfter := c.signalEvidence()
	for k, v := range filesAtCrash {
		if filesAfter[k] != v {
			t.Errorf("secret file %s changed after the restart: a competing rotation completed or reverted the partial publication", k)
		}
	}
	if reloadsAfter != reloadsAtCrash {
		t.Errorf("application reloaded %d time(s) after the restart (was %d at the crash): a competing rotation signalled it", reloadsAfter, reloadsAtCrash)
	}
	final := c.snapshot(false)
	if r, ok := final.RecordFor(c.service); !ok || r.Status != "critical_error" {
		t.Errorf("journal record for the service must stay critical_error, got %+v (found=%v)", r, ok)
	}
	c.stopGracefully(a2)
}

// 3.3 + 3.2. With an unreadable journal nothing is known about what was in flight: no
// container may be created, removed or stopped by a rotation dispatched after the
// restart, even though the secret is visible.
func testCorruptJournalBlocksRotation(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
	const point = "rolling:after_candidate_verified"
	a1 := c.startAgent(point, 1)
	c.waitReady(a1)
	c.waitProxyServing()
	c.arm()
	if _, _, nr := c.awaitFault(a1, point); nr != "" {
		t.Fatal(nr)
	}
	before := c.snapshot(false)
	if _, err := harness.CorruptJournal(c.root.State); err != nil {
		t.Fatal(err)
	}
	a2 := c.startAgent("", 0) // secret still armed
	c.waitReady(a2)
	waitFor(t, "the first poll to be refused", 60*time.Second, func() bool {
		return logContains(a2.log, "Rotation refused: the service has an unresolved operation")
	})
	time.Sleep(8 * time.Second)
	after := c.snapshot(false)
	if vs := harness.CheckCorruptJournal(before, after, harness.QuarantinedJournals(c.root.State)); len(vs) > 0 {
		t.Errorf("corrupted journal handling:\n%s", harness.FormatViolations(vs))
	}
	c.stopGracefully(a2)
}

// 3.3 control: a clean, completed rotation followed by a graceful restart must not leave
// the service blocked. Since the M1 applied-version ledger (O-5) the restarted agent's first
// poll of an UNCHANGED, already-delivered value is adopted and NOT rotated again (it used to
// dispatch a second rotation; that is deliberately gone: no mass rotation on restart). What
// must still hold, and what this control proves, is that (a) the unchanged value is not
// re-rotated, (b) the service is not refused as having an unresolved operation, and (c) a
// genuinely NEW secret value after the restart still rotates and completes.
func testInitialisationRotationStillRuns(t *testing.T, sess *dockerguard.Session, arts crashArtifacts) {
	c := newCrashCase(t, sess, arts, harness.Rolling, harness.ScenarioHappy)
	a1 := c.startAgent("", 0)
	c.waitReady(a1)
	c.waitProxyServing()
	c.arm()
	completed := func() int {
		recs, _ := harness.LoadJournal(c.root.State)
		n := 0
		for _, r := range recs {
			if r.ServiceID == c.service && r.Status == "completed" {
				n++
			}
		}
		return n
	}
	waitFor(t, "first rotation to complete", 3*time.Minute, func() bool { return completed() >= 1 })
	c.stopGracefully(a1)

	a2 := c.startAgent("", 0) // the delivered value is still visible
	c.waitReady(a2)
	waitFor(t, "the restarted agent to observe the already-delivered value without rotating it", 60*time.Second, func() bool {
		return logContains(a2.log, "Secret observed; no rotation dispatched")
	})
	if n := completed(); n != 1 {
		t.Fatalf("an unchanged, already-delivered value was rotated again after the restart (completed rotations: %d)", n)
	}
	if logContains(a2.log, "Rotation refused: the service has an unresolved operation") {
		t.Errorf("a settled service was refused:\n%s", logTail(a2.log, 40))
	}

	// A new value still rotates through the restarted agent.
	c.vault.Set(crashSecret, map[string]string{"password": syntheticValue("v3")})
	waitFor(t, "a changed secret to rotate through the restarted agent", 3*time.Minute, func() bool { return completed() >= 2 })
	if logContains(a2.log, "Rotation refused: the service has an unresolved operation") {
		t.Errorf("a settled service was refused:\n%s", logTail(a2.log, 40))
	}
	c.stopGracefully(a2)
}
