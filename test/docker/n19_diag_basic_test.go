//go:build dockerint

package docker

// SECTION 1 / 3 (see the section map in n19_diag_helpers_test.go).
//
// LEGACY ARCHIVE-BEHAVIOUR DIAGNOSTICS: scenarios A-F, H-J and R exercise the Docker
// ARCHIVE endpoint (rotation.StreamSecretToContainer, `docker cp`) on created and running
// containers. The managed rotation no longer uses that endpoint; these scenarios stay
// because they are the only runtime evidence about the premise (archive uploads into a
// created container's writable layer vs the tmpfs) and they must keep answering it. They
// RECORD, never assert the hoped-for outcome (see n19_diag_helpers_test.go for the
// contract). Scenarios D and G also belong to RUNTIME MOUNT VERIFICATION: D records how the
// declared tmpfs appears at start (the fact VerifyRunningGate relies on) and G records the
// hardened exec injector with RequireTmpfs (the delivery primitive of the gated path).
// Scenarios K-M (managed rotation through the real controller, gated delivery) are in
// n19_diag_managed_test.go. Compiled by `go vet -tags dockerint`; NOT executed by it.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

// n19Running records the running-tmpfs observation of path in container id.
func n19Running(t *testing.T, sc, observation, operation, id, want string) n19Obs {
	t.Helper()
	sess := dockerguard.Current(t)
	o := n19Base(t, sc, id)
	o.Observation, o.Operation, o.Location = observation, operation, "running_tmpfs"
	got, d := n19ExecHash(sess, t, id, n19File)
	o.CanaryIntegrity = n19Integrity(got, want)
	o.FileVisibility = n19Visibility(o.CanaryIntegrity)
	o.Detail = strings.TrimSpace("fstype=" + n19FSType(o.MountInfo) + " " + d)
	return o
}

// n19Listing appends `ls -la` of the secret dir (names/modes only) to the detail.
func n19Listing(t *testing.T, id string) string {
	t.Helper()
	out, err := dockerguard.Current(t).Docker(t, "exec", id, "ls", "-la", n19Dir)
	if err != nil {
		return "ls failed: " + n19Clip(err.Error(), 100)
	}
	return "ls: " + n19Clip(out, 260)
}

// A: a created-but-not-started container with a declared tmpfs.
func TestN19Diag_A_CreatedNotStartedWithTmpfs(t *testing.T) {
	lv := n19Begin(t, "A")
	c := n19Create(t, lv, true, injIdleCmd)
	o := n19Base(t, "A", c.id)
	o.Observation, o.Operation, o.Location = "created_declared", "docker create --tmpfs "+n19Dir, "pre_start"
	o.Detail = "state must be 'created'; no runtime mountinfo exists before start"
	n19Emit(t, o)
	if o.LifecycleState != "created" {
		t.Fatalf("fixture is %q, expected created", o.LifecycleState)
	}
}

// B: archive injection (the real production helper) into the created container,
// then a layer read through the archive API, still WITHOUT starting it.
func TestN19Diag_B_ArchiveInjectBeforeStart(t *testing.T) {
	lv := n19Begin(t, "B")
	canary, want := n19Canary("B")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)
	o := n19Base(t, "B", c.id)
	o.Observation, o.Operation, o.Location = "archive_inject_result", "rotation.StreamSecretToContainer on created-not-started container", "pre_start"
	o.Detail = "inject " + n19ErrText(injErr)
	n19Emit(t, o)

	integrity, d := n19LayerRead(t, c.id, n19File, want)
	o = n19Base(t, "B", c.id)
	o.Observation, o.Operation, o.Location = "layer_read_unstarted", "CopyFromContainer(file) on the NOT started container (no tmpfs mounted: reads the layer)", "layer"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = integrity, n19Visibility(integrity), d
	n19Emit(t, o)
}

// C: the underlying destination of the injection, without starting, and what
// `docker diff` shows before and after start (both recorded, neither asserted).
func TestN19Diag_C_LayerInspectionWithoutStart(t *testing.T) {
	lv := n19Begin(t, "C")
	canary, want := n19Canary("C")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)

	o := n19Base(t, "C", c.id)
	o.Observation, o.Operation, o.Location = "dir_listing_unstarted", "CopyFromContainer(dir) on the not-started container", "layer"
	o.Detail = "inject " + n19ErrText(injErr) + "; " + n19LayerList(t, c.id, n19Dir)
	n19Emit(t, o)

	integrity, d := n19LayerRead(t, c.id, n19File, want)
	o = n19Base(t, "C", c.id)
	o.Observation, o.Operation, o.Location = "file_read_unstarted", "CopyFromContainer(file) on the not-started container", "layer"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = integrity, n19Visibility(integrity), d
	n19Emit(t, o)

	lines, present, derr := n19Diff(t, c.id)
	o = n19Base(t, "C", c.id)
	o.Observation, o.Operation, o.Location = "diff_before_start", "docker diff (container layer changes) before start", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	if derr != nil {
		o.Detail = "diff error: " + derr.Error()
	}
	n19Emit(t, o)

	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	lines, present, derr = n19Diff(t, c.id)
	o = n19Base(t, "C", c.id)
	o.Observation, o.Operation, o.Location = "diff_after_start", "docker diff after start (tmpfs mounted over the dir)", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	if derr != nil {
		o.Detail = "diff error: " + derr.Error()
	}
	n19Emit(t, o)
}

// D: start the container and inspect the mounted tmpfs.
func TestN19Diag_D_StartAndInspectTmpfs(t *testing.T) {
	lv := n19Begin(t, "D")
	canary, want := n19Canary("D")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	o := n19Running(t, "D", "running_tmpfs_after_start", "inject (archive, pre-start) -> start -> exec sha256 of the file; mountinfo of the dir", c.id, want)
	o.Detail += "; inject " + n19ErrText(injErr) + "; " + n19Listing(t, c.id)
	n19Emit(t, o)
	if n19FSType(o.MountInfo) != "tmpfs" {
		// Recorded as evidence; the diagnostic's premise (a tmpfs is mounted) is
		// itself an observation, not an assertion.
		t.Logf("note: the secret directory is not reported as tmpfs by mountinfo (see evidence)")
	}
}

// E: can the application read the expected secret?
func TestN19Diag_E_ApplicationReadsSecret(t *testing.T) {
	lv := n19Begin(t, "E")
	canary, want := n19Canary("E")
	c := n19Create(t, lv, true, n19App)
	injErr := n19ArchiveInject(t, c.id, canary)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	present, size, sha, ok := n19WaitAppLine(t, c.id)
	o := n19Base(t, "E", c.id)
	o.Observation, o.Operation, o.Location = "application_view", "busybox loop reading the file (logs presence/size/sha256 only)", "app_view"
	switch {
	case !ok:
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "unknown", "unknown", "application produced no N19-APP line"
	case !present:
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "absent", "absent", "application reports present=0; inject "+n19ErrText(injErr)
	default:
		o.CanaryIntegrity, o.FileVisibility = n19Integrity(sha, want), "present"
		o.Detail = "application reports present=1 size=" + size + "; inject " + n19ErrText(injErr)
	}
	n19Emit(t, o)
}

// F: archive injection into a RUNNING container with an active tmpfs.
func TestN19Diag_F_ArchiveInjectIntoRunning(t *testing.T) {
	lv := n19Begin(t, "F")
	canary, want := n19Canary("F")
	c := n19Create(t, lv, true, injIdleCmd)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	injErr := n19ArchiveInject(t, c.id, canary)
	o := n19Running(t, "F", "running_tmpfs_after_archive_inject", "rotation.StreamSecretToContainer into a RUNNING container with an active tmpfs", c.id, want)
	o.Detail += "; inject " + n19ErrText(injErr)
	n19Emit(t, o)

	integrity, d := n19LayerRead(t, c.id, n19File, want)
	o = n19Base(t, "F", c.id)
	o.Observation, o.Operation, o.Location = "archive_read_running", "CopyFromContainer(file) on the running container (may or may not expose tmpfs)", "running_archive_view"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = integrity, n19Visibility(integrity), d
	n19Emit(t, o)

	lines, present, derr := n19Diff(t, c.id)
	o = n19Base(t, "F", c.id)
	o.Observation, o.Operation, o.Location = "diff_after_running_inject", "docker diff after the archive inject", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	n19Emit(t, o)
}

// G: the hardened exec injector (RequireTmpfs) into a running container.
func TestN19Diag_G_ExecInjectRequireTmpfs(t *testing.T) {
	lv := n19Begin(t, "G")
	canary, want := n19Canary("G")
	c := n19Create(t, lv, true, injIdleCmd)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	injErr := injector.InjectFilesWithOptions(ctx, dockerClient(t), c.id, map[string]string{n19Key: canary}, n19UID, n19UID,
		injector.Options{Dir: n19Dir, RequireTmpfs: true})
	o := n19Running(t, "G", "running_tmpfs_after_exec_inject", "injector.InjectFilesWithOptions{RequireTmpfs:true} into a running container", c.id, want)
	o.Detail += "; inject " + n19ErrText(injErr)
	n19Emit(t, o)

	lines, present, derr := n19Diff(t, c.id)
	o = n19Base(t, "G", c.id)
	o.Observation, o.Operation, o.Location = "diff_after_exec_inject", "docker diff after the exec inject (expected: nothing under the secret dir)", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	n19Emit(t, o)
}

// H: stop and restart: what is visible, does a layer copy resurface?
func TestN19Diag_H_StopRestartBehaviour(t *testing.T) {
	lv := n19Begin(t, "H")
	canary, want := n19Canary("H")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	o := n19Running(t, "H", "running_before_stop", "pre-start archive inject -> start", c.id, want)
	o.Detail += "; inject " + n19ErrText(injErr)
	n19Emit(t, o)

	if err := n19Stop(t, c.id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	integrity, d := n19LayerRead(t, c.id, n19File, want)
	o = n19Base(t, "H", c.id)
	o.Observation, o.Operation, o.Location = "stopped_layer_read", "CopyFromContainer(file) on the STOPPED container (tmpfs gone)", "layer_after_stop"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = integrity, n19Visibility(integrity), d
	n19Emit(t, o)

	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("restart (start): %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running after start")
	}
	o = n19Running(t, "H", "running_after_stop_start", "stop -> start (tmpfs is recreated)", c.id, want)
	n19Emit(t, o)

	if err := n19Restart(t, c.id); err != nil {
		t.Fatalf("docker restart: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running after restart")
	}
	o = n19Running(t, "H", "running_after_docker_restart", "docker restart", c.id, want)
	n19Emit(t, o)
}

// I: plaintext beneath the tmpfs: after stop (tmpfs gone), read the container's own
// layer through the archive API and `docker diff`. docker commit is NOT used (it
// would create a persistent copy). A control (exec inject then stop) is included.
func TestN19Diag_I_PersistentPlaintextBeneathTmpfs(t *testing.T) {
	lv := n19Begin(t, "I")
	canary, want := n19Canary("I")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	if err := n19Stop(t, c.id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	integrity, d := n19LayerRead(t, c.id, n19File, want)
	o := n19Base(t, "I", c.id)
	o.Observation, o.Operation, o.Location = "hypothesis_path_after_stop", "archive pre-start inject -> start -> stop -> CopyFromContainer(file) on the stopped container", "layer"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = integrity, n19Visibility(integrity), d+"; inject "+n19ErrText(injErr)
	n19Emit(t, o)

	lines, present, derr := n19Diff(t, c.id)
	o = n19Base(t, "I", c.id)
	o.Observation, o.Operation, o.Location = "diff_after_stop", "docker diff of the stopped container", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	n19Emit(t, o)

	// Control: the hardened exec path must leave nothing in the layer.
	ccanary, cwant := n19Canary("I")
	cc := n19Create(t, lv, true, injIdleCmd)
	if err := n19Start(t, cc.id); err != nil {
		t.Fatalf("control start: %v", err)
	}
	if !n19WaitRunning(t, cc.id) {
		t.Fatalf("control did not stay running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cinj := injector.InjectFilesWithOptions(ctx, dockerClient(t), cc.id, map[string]string{n19Key: ccanary}, n19UID, n19UID,
		injector.Options{Dir: n19Dir, RequireTmpfs: true})
	if err := n19Stop(t, cc.id); err != nil {
		t.Fatalf("control stop: %v", err)
	}
	ci, cd := n19LayerRead(t, cc.id, n19File, cwant)
	o = n19Base(t, "I", cc.id)
	o.Observation, o.Operation, o.Location = "control_exec_inject_then_stop", "CONTROL: exec inject (RequireTmpfs) into running -> stop -> CopyFromContainer(file)", "control_layer"
	o.CanaryIntegrity, o.FileVisibility, o.Detail = ci, n19Visibility(ci), cd+"; inject "+n19ErrText(cinj)
	n19Emit(t, o)
}

// J: removal and scoped cleanup (guard labels); nothing with the run labels remains.
func TestN19Diag_J_RemovalAndScopedCleanup(t *testing.T) {
	lv := n19Begin(t, "J")
	canary, _ := n19Canary("J")
	c := n19Create(t, lv, true, injIdleCmd)
	injErr := n19ArchiveInject(t, c.id, canary)
	if err := n19Start(t, c.id); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	res := n19RemoveByLabel(t, lv)

	dc := dockerClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, ierr := dc.ContainerInspect(ctx, c.id)
	gone := "still-exists"
	if ierr != nil && client.IsErrNotFound(ierr) {
		gone = "not-found"
	} else if ierr != nil {
		gone = "inspect-error"
	}
	runOnly := dockerguard.Current(t).Labels()
	// Anything still carrying the run labels AND any n19 label (other tests' leftovers).
	left, lerr := dc.ContainerList(ctx, container.ListOptions{All: true, Filters: n19RunLabelFilters(runOnly, true)})
	o := n19Base(t, "J", "")
	o.Container = n19Short(c.id)
	o.Observation, o.Operation, o.Location = "removal_and_scoped_cleanup", "inject (archive) -> start -> force-remove by guard labels + dso.it.n19 -> verify", "none"
	o.LifecycleState = "removed"
	o.Cleanup = res + " inspect-after=" + gone
	o.Detail = "inject " + n19ErrText(injErr) + "; n19-labelled containers of this run remaining after cleanup: "
	if lerr != nil {
		o.Detail += "unknown (" + lerr.Error() + ")"
	} else {
		o.Detail += fmt.Sprint(len(left))
	}
	n19Emit(t, o)
}

// R: LEGACY ARCHIVE-BEHAVIOUR reproducer (section 1). Minimal pure-Docker reproducer. NO DSO code: docker create + docker cp (tar on
// stdin, same absolute header name production builds) + docker start, through the
// guarded endpoint. It supplements the production-path scenarios; it does not
// replace them and does not feed the n19_verdict.
func TestN19Diag_R_MinimalReproducer(t *testing.T) {
	lv := n19Begin(t, "R")
	canary, want := n19Canary("R")
	c := n19Create(t, lv, true, injIdleCmd)

	_, se, cpErr := n19Docker(t, n19Tar(n19File, canary, n19UID), "cp", "-", c.id+":/")
	o := n19Base(t, "R", c.id)
	o.Observation, o.Operation, o.Location = "cp_into_created", "docker cp - CONTAINER:/ (tar on stdin) into the created container", "pre_start"
	if cpErr != nil {
		o.Detail = "cp error: " + n19Clip(injRedact(se), 200)
	} else {
		o.Detail = "cp ok"
	}
	n19Emit(t, o)

	out, _, rerr := n19Docker(t, nil, "cp", c.id+":"+n19File, "-")
	o = n19Base(t, "R", c.id)
	o.Observation, o.Operation, o.Location = "cp_out_unstarted", "docker cp CONTAINER:file - on the not-started container", "layer"
	if rerr != nil {
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "absent", "absent", "docker cp out failed (path not found or error)"
	} else if h, ok := n19TarFileHash(out); ok {
		o.CanaryIntegrity = n19Integrity(h, want)
		o.FileVisibility = n19Visibility(o.CanaryIntegrity)
	} else {
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "unknown", "unknown", "no regular file in the tar"
	}
	n19Emit(t, o)

	if _, se, err := n19Docker(t, nil, "start", c.id); err != nil {
		t.Fatalf("docker start: %v %s", err, se)
	}
	if !n19WaitRunning(t, c.id) {
		t.Fatalf("container did not stay running")
	}
	o = n19Running(t, "R", "running_tmpfs_after_start", "docker start -> exec sha256 of the file", c.id, want)
	n19Emit(t, o)

	lines, present, derr := n19Diff(t, c.id)
	o = n19Base(t, "R", c.id)
	o.Observation, o.Operation, o.Location = "diff_after_start", "docker diff after start", "layer_diff"
	o.LayerInspection = n19DiffState(present, derr)
	o.Detail = "entries=" + strings.Join(lines, ",")
	n19Emit(t, o)

	if _, _, err := n19Docker(t, nil, "stop", "-t", "5", c.id); err != nil {
		t.Fatalf("docker stop: %v", err)
	}
	out, _, rerr = n19Docker(t, nil, "cp", c.id+":"+n19File, "-")
	o = n19Base(t, "R", c.id)
	o.Observation, o.Operation, o.Location = "cp_out_after_stop", "docker cp CONTAINER:file - on the stopped container", "layer_after_stop"
	if rerr != nil {
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "absent", "absent", "docker cp out failed (path not found or error)"
	} else if h, ok := n19TarFileHash(out); ok {
		o.CanaryIntegrity = n19Integrity(h, want)
		o.FileVisibility = n19Visibility(o.CanaryIntegrity)
	} else {
		o.CanaryIntegrity, o.FileVisibility, o.Detail = "unknown", "unknown", "no regular file in the tar"
	}
	n19Emit(t, o)
}
