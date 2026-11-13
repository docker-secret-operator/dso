//go:build dockerint

package docker

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

// M0-04 recovery against a REAL daemon. These tests construct the container layouts
// a crash can leave behind and run the real AutomaticRecovery against them. They do
// NOT kill a DSO process mid-rotation: that needs fault-injection points in the
// rotation path that do not exist yet (see docs/design/m0-04-recovery-design.md), so
// "recovery after real process termination" remains UNVERIFIED.

func runOwned(t *testing.T, name string, healthy bool) string {
	t.Helper()
	sess := dockerguard.Current(t)
	args := []string{"run", "-d", "--name", name}
	args = append(args, sess.LabelArgs()...)
	args = append(args, "--label", "dso.reloader=true", "--label", "dso.owner="+ownerID)
	if healthy {
		args = append(args, "--health-cmd", "true", "--health-interval", "1s", "--health-retries", "1")
	}
	args = append(args, testImage, "sh", "-c", "trap 'exit 0' TERM; while true; do sleep 1 & wait $!; done")
	return dockerCmd(t, args...)
}

func waitHealthy(t *testing.T, id string) {
	t.Helper()
	waitFor(t, "container "+id[:12]+" healthy", 60*time.Second, func() bool {
		out, err := dockerguard.Current(t).Docker(t, "inspect", "-f", "{{.State.Health.Status}}", id)
		return err == nil && out == "healthy"
	})
}

func exists(t *testing.T, name string) bool {
	t.Helper()
	_, err := dockerguard.Current(t).Docker(t, "inspect", "-f", "{{.Id}}", name)
	return err == nil
}

func newRecovery(t *testing.T, originals ...string) *agent.AutomaticRecovery {
	t.Helper()
	logger := zaptest.NewLogger(t)
	st, err := agent.NewStateTracker(t.TempDir(), logger)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartRotation("vault", secretName, strings.Join(originals, ","), ""); err != nil {
		t.Fatal(err)
	}
	ar := agent.NewAutomaticRecovery(dockerClient(t), logger, st)
	ar.OwnerID = ownerID
	return ar
}

func TestRecovery_RealDaemon_SwapNotYetCommitted_KeepsOriginalRemovesCandidate(t *testing.T) {
	base := uniqueName(t, "dso-it-rec-a")
	orig := runOwned(t, base, true)
	runOwned(t, base+"_dso_new_1700000000", true)
	waitHealthy(t, orig)
	if err := newRecovery(t, orig).RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !exists(t, base) || exists(t, base+"_dso_new_1700000000") {
		t.Fatal("original must remain and the candidate must be removed")
	}
}

func TestRecovery_RealDaemon_SwapCompleted_KeepsPromotedRemovesBackup(t *testing.T) {
	base := uniqueName(t, "dso-it-rec-b")
	old := runOwned(t, base+"_dso_backup_1700000000", true)
	promoted := runOwned(t, base, true)
	waitHealthy(t, promoted)
	if err := newRecovery(t, old).RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !exists(t, base) || exists(t, base+"_dso_backup_1700000000") {
		t.Fatal("the promoted workload must remain and the old backup must be removed")
	}
}

func TestRecovery_RealDaemon_BetweenRenames_RemovesNothing(t *testing.T) {
	base := uniqueName(t, "dso-it-rec-c")
	old := runOwned(t, base+"_dso_backup_1700000000", true)
	runOwned(t, base+"_dso_new_1700000000", true)
	if err := newRecovery(t, old).RecoverFromCrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !exists(t, base+"_dso_backup_1700000000") || !exists(t, base+"_dso_new_1700000000") {
		t.Fatal("with no healthy holder of the real name BOTH containers must be preserved")
	}
}
