//go:build dockerint

package docker

// N19 DIAGNOSTIC helpers (docs/audit/42-N19-DIAGNOSTIC-PLAN.md).
//
// Finding N19: file-mode managed delivery USED rotation.StreamSecretToContainer
// (the Docker archive endpoint) on CREATED-NOT-STARTED containers whose
// /run/secrets/dso is a declared tmpfs. Daemon source suggests the archive lands
// in the writable layer and is hidden once the tmpfs mounts at start. This file
// holds the shared machinery of the TestN19Diag_* scenarios.
//
// STATUS OF THE PRODUCT PATH (docs/design/n19-managed-delivery.md): the managed
// rotation paths NO LONGER call the archive endpoint. They create the candidate with a
// startup gate, verify it, deliver through the hardened exec injector (RequireTmpfs),
// probe, journal the evidence and only then release the gate. That replacement is
// IMPLEMENTED BUT RUNTIME-UNVERIFIED: nothing below has been run against a real daemon
// for it, and compiling these files (go vet -tags dockerint) is not execution.
//
// SECTION MAP (the 14 scenarios and their names are unchanged; run-all.sh lists them):
//
//	1 LEGACY ARCHIVE-BEHAVIOUR DIAGNOSTICS   A B C D E F H I J and the minimal reproducer R
//	  (what the Docker archive endpoint does to a created container; these keep answering the
//	  premise question and are NOT a test of the managed path)
//	2 GATED-DELIVERY FUNCTIONAL              K L M (n19_diag_managed_test.go): the real
//	  ReloaderController rotation with the gated candidate + exec delivery
//	3 RUNTIME MOUNT VERIFICATION             D (tmpfs mounted at start) and G (exec injector,
//	  RequireTmpfs) are the recorded runtime facts the gate's inspect/tmpfs checks rely on
//	4 CRASH/RECOVERY                         test/docker/crash_recovery_test.go (TestCrash*,
//	  -tags 'dockerint dsofault'); no gated-phase crash scenario is driven against a real
//	  daemon yet (the Docker-free journal/recovery tests cover the phases)
//	5 RESTART RE-DELIVERY                    NO guarded scenario yet: re-delivery after a
//	  daemon restart, agent-start reconciliation and the exit-75 diagnostic are covered by
//	  Docker-free tests only (internal/watcher n19_reconcile_test.go); still [U] at runtime
//	6 SECRET-VERSION RECONCILIATION          NO guarded scenario yet: dispatch-vs-delivered
//	  identity and legacy-record handling are covered by Docker-free tests only
//	  (internal/agent n19_followup_test.go)
//
// A section with no guarded scenario must not be reported as executed or qualified.
//
// These are DIAGNOSTICS, not assertions of the hoped-for behaviour: they RECORD
// what the daemon does, one structured evidence line per observation:
//
//	N19-EVIDENCE {json}
//
// A scenario fails only when an observation could not be made at all (e.g. the
// fixture cannot be created). A demonstrated N19 failure is a PASSING diagnostic
// whose evidence says so; scripts/integration/m0-lib.sh turns the evidence into
// the `n19_verdict` finding.
//
// Safety: everything goes through the isolation guard (verified endpoint, run
// labels on every resource, unique names, scoped removal); canaries are synthetic
// and are NEVER printed, logged or placed in evidence: only sha256 digests,
// presence and sizes are. No docker commit, no host-path inspection, no write into
// Docker's storage.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/test/dockerguard"
)

const (
	n19Prefix = "N19-EVIDENCE "
	n19Dir    = "/run/secrets/dso"
	n19Key    = "value"
	n19File   = n19Dir + "/" + n19Key
	n19UID    = 10001
	n19Label  = "dso.it.n19"
	// n19App logs ONLY presence, size and sha256 of the file the application is
	// supposed to read. It never prints the content.
	n19App = `f=/run/secrets/dso/value
trap 'exit 0' TERM
while true; do
  if [ -e "$f" ]; then
    echo "N19-APP present=1 size=$(wc -c < "$f" 2>/dev/null || echo 0) sha256=$(sha256sum "$f" 2>/dev/null | cut -d' ' -f1)"
  else
    echo "N19-APP present=0 size=0 sha256=-"
  fi
  sleep 1 & wait $!
done`
	// n19IdleApp never touches the secret file (scenario M: healthy regardless).
	n19IdleApp = injIdleCmd
)

// n19Obs is one evidence record. Field meanings are fixed: the runner's verdict
// function (m0_n19_verdict) reads scenario, location and canary_integrity.
type n19Obs struct {
	Scenario        string            `json:"scenario"`    // single letter A..M, R
	Observation     string            `json:"observation"` // stable name of what was observed
	Operation       string            `json:"operation"`   // what was done to make the observation
	Location        string            `json:"location"`    // layer | running_tmpfs | app_view | shadow_tmpfs | layer_diff | control_layer | pre_start | none | ...
	DockerServer    string            `json:"docker_server"`
	APIVersion      string            `json:"api_version"`
	StorageDriver   string            `json:"storage_driver"`
	Snapshotter     string            `json:"snapshotter"` // io.containerd.snapshotter.v1 when the containerd image store is in use; "" otherwise
	Container       string            `json:"container"`   // short id
	RunID           string            `json:"run_id"`
	DeclaredTmpfs   map[string]string `json:"declared_tmpfs"`
	DeclaredMounts  []string          `json:"declared_mounts"`
	MountInfo       []string          `json:"runtime_mountinfo"` // filtered to the secret directory; only when running
	LifecycleState  string            `json:"lifecycle_state"`
	FileVisibility  string            `json:"file_visibility"`  // present | absent | unknown | n/a
	CanaryIntegrity string            `json:"canary_integrity"` // match | mismatch | absent | unknown | n/a (sha256 vs expected; never the value)
	LayerInspection string            `json:"layer_inspection"` // present | absent | not_checked | unknown
	Cleanup         string            `json:"cleanup"`
	Detail          string            `json:"detail"`
}

type n19Env struct{ server, api, driver, snapshotter string }

var (
	n19EnvOnce sync.Once
	n19EnvVal  n19Env
)

func n19Environment(t *testing.T) n19Env {
	t.Helper()
	dc := dockerClient(t)
	n19EnvOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		e := n19Env{server: "unknown", api: "unknown", driver: "unknown"}
		if v, err := dc.ServerVersion(ctx); err == nil {
			e.server, e.api = v.Version, v.APIVersion
		}
		if info, err := dc.Info(ctx); err == nil {
			e.driver = info.Driver
			for _, kv := range info.DriverStatus {
				if kv[0] == "driver-type" {
					e.snapshotter = kv[1]
				}
			}
		}
		n19EnvVal = e
	})
	return n19EnvVal
}

func n19Clip(s string, n int) string {
	s = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(s)
	if len(s) > n {
		return s[:n] + "...(clipped)"
	}
	return s
}

func n19Sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// n19Emit writes one evidence line. The canary can never reach it: Detail is
// redacted against the run sentinel and every other field is structural.
func n19Emit(t *testing.T, o n19Obs) {
	t.Helper()
	e := n19Environment(t)
	o.DockerServer, o.APIVersion, o.StorageDriver, o.Snapshotter = e.server, e.api, e.driver, e.snapshotter
	o.RunID = dockerguard.Current(t).Config.Labels()["dso.test.run"]
	o.Detail = n19Clip(injRedact(o.Detail), 600)
	if o.FileVisibility == "" {
		o.FileVisibility = "n/a"
	}
	if o.CanaryIntegrity == "" {
		o.CanaryIntegrity = "n/a"
	}
	if o.LayerInspection == "" {
		o.LayerInspection = "not_checked"
	}
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("N19 evidence could not be encoded: %v", err)
	}
	t.Logf("%s%s", n19Prefix, b)
}

// n19Base fills the container-derived fields: short id, declared tmpfs/mounts,
// lifecycle state and (only while running) the mountinfo lines of the secret dir.
func n19Base(t *testing.T, sc, id string) n19Obs {
	t.Helper()
	o := n19Obs{Scenario: sc, Container: n19Short(id), LifecycleState: "unknown", LayerInspection: "not_checked"}
	if id == "" {
		return o
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	in, err := dockerClient(t).ContainerInspect(ctx, id)
	if err != nil {
		if client.IsErrNotFound(err) {
			o.LifecycleState = "removed"
		} else {
			o.Detail = "inspect: " + err.Error()
		}
		return o
	}
	if in.State != nil {
		o.LifecycleState = in.State.Status
	}
	if in.HostConfig != nil {
		o.DeclaredTmpfs = in.HostConfig.Tmpfs
		for _, m := range in.HostConfig.Mounts {
			o.DeclaredMounts = append(o.DeclaredMounts, fmt.Sprintf("%s:%s", m.Type, m.Target))
		}
	}
	for _, m := range in.Mounts {
		o.DeclaredMounts = append(o.DeclaredMounts, fmt.Sprintf("inspect:%s:%s", m.Type, m.Destination))
	}
	if in.State != nil && in.State.Running {
		o.MountInfo = n19MountInfo(t, id)
	}
	return o
}

func n19Short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// n19MountInfo returns the mountinfo lines whose mount point is the secret dir.
func n19MountInfo(t *testing.T, id string) []string {
	t.Helper()
	out, err := dockerguard.Current(t).Docker(t, "exec", id, "cat", "/proc/self/mountinfo")
	if err != nil {
		return []string{"mountinfo unavailable: " + n19Clip(err.Error(), 120)}
	}
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) > 4 && f[4] == n19Dir {
			keep = append(keep, n19Clip(l, 300))
		}
	}
	if len(keep) == 0 {
		return []string{"(no mount at " + n19Dir + ")"}
	}
	return keep
}

// n19FSType extracts the filesystem type from the mountinfo lines ("-" separator).
func n19FSType(lines []string) string {
	for _, l := range lines {
		f := strings.Fields(l)
		for i, w := range f {
			if w == "-" && i+1 < len(f) {
				return f[i+1]
			}
		}
	}
	return "none"
}

// n19Integrity compares a sha256 hex digest against the expected one.
func n19Integrity(got, want string) string {
	switch {
	case got == "N19ABSENT":
		return "absent"
	case got == "":
		return "unknown"
	case got == want:
		return "match"
	default:
		return "mismatch"
	}
}

func n19Visibility(integrity string) string {
	switch integrity {
	case "absent":
		return "absent"
	case "match", "mismatch":
		return "present"
	}
	return "unknown"
}

// n19ExecHash reports the sha256 of path as seen INSIDE the (running) container,
// or N19ABSENT when it does not exist. The content is never emitted.
func n19ExecHash(sess *dockerguard.Session, t *testing.T, id, path string) (got, detail string) {
	out, err := sess.Docker(t, "exec", id, "sh", "-c",
		`if [ -e "$1" ]; then sha256sum "$1" | cut -d' ' -f1; else echo N19ABSENT; fi`, "sh", path)
	if err != nil {
		return "", "exec failed: " + n19Clip(err.Error(), 160)
	}
	out = strings.TrimSpace(out)
	if out != "N19ABSENT" && !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(out) {
		return "", "unexpected exec output (length " + fmt.Sprint(len(out)) + ")"
	}
	return out, ""
}

// n19LayerRead reads path through the Docker ARCHIVE API (CopyFromContainer) and
// hashes the first regular file. On a stopped or not-yet-started container this
// reads the container's own layer (no tmpfs is mounted); on a running container
// the daemon may or may not expose tmpfs content through it: the evidence records
// which. It never prints content.
func n19LayerRead(t *testing.T, id, path, want string) (integrity, detail string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, _, err := dockerClient(t).CopyFromContainer(ctx, id, path)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "absent", "archive API: path not found"
		}
		return "unknown", "archive API error: " + n19Clip(err.Error(), 200)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "unknown", "archive read error: " + n19Clip(err.Error(), 160)
		}
		if hdr.Typeflag == tar.TypeReg {
			h := sha256.New()
			if _, err := io.Copy(h, io.LimitReader(tr, 1<<20)); err != nil {
				return "unknown", "archive copy error"
			}
			return n19Integrity(hex.EncodeToString(h.Sum(nil)), want),
				fmt.Sprintf("regular file size=%d mode=%o uid=%d gid=%d", hdr.Size, hdr.Mode&0o7777, hdr.Uid, hdr.Gid)
		}
	}
	return "unknown", "archive contained no regular file"
}

// n19LayerList lists the entries (name, mode, uid) of dir as the archive API sees it.
func n19LayerList(t *testing.T, id, dir string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rc, _, err := dockerClient(t).CopyFromContainer(ctx, id, dir)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "directory not found via archive API"
		}
		return "archive API error: " + n19Clip(err.Error(), 160)
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	var ents []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		ents = append(ents, fmt.Sprintf("%s mode=%o uid=%d", hdr.Name, hdr.Mode&0o7777, hdr.Uid))
	}
	if len(ents) == 0 {
		return "archive contained no entries"
	}
	return strings.Join(ents, "; ")
}

// n19Diff returns the `docker diff` (writable-layer change) lines that concern
// the secrets path, and whether the secret file itself is among them.
func n19Diff(t *testing.T, id string) (lines []string, filePresent bool, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := dockerClient(t).ContainerDiff(ctx, id)
	if err != nil {
		return nil, false, err
	}
	for _, c := range ch {
		k := map[container.ChangeType]string{0: "C", 1: "A", 2: "D"}[c.Kind]
		if strings.Contains(c.Path, "run/secrets") {
			lines = append(lines, k+" "+c.Path)
			if c.Path == n19File {
				filePresent = true
			}
		}
	}
	return lines, filePresent, nil
}

func n19DiffState(present bool, err error) string {
	switch {
	case err != nil:
		return "unknown"
	case present:
		return "present"
	}
	return "absent"
}

// n19ArchiveInject delivers the canary with the REAL production helper used by
// the rolling and restart strategies (rotation.StreamSecretToContainer).
func n19ArchiveInject(t *testing.T, id, canary string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rotation.StreamSecretToContainer(ctx, dockerClient(t), id, n19Dir, map[string]string{n19Key: canary}, n19UID, n19UID)
}

func n19ErrText(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + n19Clip(injRedact(err.Error()), 300)
}

// ---- fixtures ---------------------------------------------------------------

type n19Ctr struct {
	id, name, lv string
}

// n19Begin returns the unique label value for this scenario and registers the
// scoped cleanup (only containers carrying the guard labels AND this label value),
// whose result is itself recorded as evidence.
func n19Begin(t *testing.T, sc string) string {
	t.Helper()
	n19Environment(t)
	lv := uniqueName(t, "n19-"+strings.ToLower(sc))
	t.Cleanup(func() {
		res := n19RemoveByLabel(t, lv)
		o := n19Obs{Scenario: sc, Observation: "cleanup", Operation: "force-remove containers labelled " + n19Label + "=<this test> and the guard labels", Location: "none", Cleanup: res}
		n19Emit(t, o)
	})
	return lv
}

func n19LabelFilters(t *testing.T, lv string) filters.Args {
	f := filters.NewArgs()
	f.Add("label", n19Label+"="+lv)
	for k, v := range dockerguard.Current(t).Labels() {
		f.Add("label", k+"="+v)
	}
	return f
}

// n19RunLabelFilters selects containers carrying every guard label (and, when
// requireN19 is set, the dso.it.n19 label key with any value).
func n19RunLabelFilters(labels map[string]string, requireN19 bool) filters.Args {
	f := filters.NewArgs()
	for k, v := range labels {
		f.Add("label", k+"="+v)
	}
	if requireN19 {
		f.Add("label", n19Label)
	}
	return f
}

// n19RemoveByLabel force-removes this scenario's containers and reports.
func n19RemoveByLabel(t *testing.T, lv string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dc := dockerClient(t)
	list, err := dc.ContainerList(ctx, container.ListOptions{All: true, Filters: n19LabelFilters(t, lv)})
	if err != nil {
		return "list failed: " + n19Clip(err.Error(), 160)
	}
	removed := 0
	var errs []string
	for _, c := range list {
		if err := dc.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !client.IsErrNotFound(err) {
			errs = append(errs, n19Clip(err.Error(), 100))
			continue
		}
		removed++
	}
	left, lerr := dc.ContainerList(ctx, container.ListOptions{All: true, Filters: n19LabelFilters(t, lv)})
	if lerr != nil {
		return fmt.Sprintf("removed=%d verify-list failed: %s", removed, n19Clip(lerr.Error(), 120))
	}
	res := fmt.Sprintf("removed=%d remaining=%d", removed, len(left))
	if len(errs) > 0 {
		res += " errors=" + strings.Join(errs, "|")
	}
	return res
}

func n19LastToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

var n19IDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// n19Create makes a labelled container WITHOUT starting it (docker create via the
// guarded CLI). tmpfs declares /run/secrets/dso exactly as the DSO compose rewrite
// does (internal/compose/ast.go AddTmpfsMount: the bare path, default options).
func n19Create(t *testing.T, lv string, tmpfs bool, script string, extra ...string) n19Ctr {
	t.Helper()
	name := uniqueName(t, "dso-it-n19")
	args := []string{"create", "--name", name}
	args = append(args, dockerguard.Current(t).LabelArgs()...)
	args = append(args, "--label", n19Label+"="+lv)
	args = append(args, extra...)
	if tmpfs {
		args = append(args, "--tmpfs", n19Dir)
	}
	args = append(args, testImage, "sh", "-c", script)
	id := n19LastToken(dockerCmd(t, args...))
	if !n19IDRe.MatchString(id) {
		t.Fatalf("docker create did not return a container id")
	}
	return n19Ctr{id: id, name: name, lv: lv}
}

func n19Start(t *testing.T, id string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return dockerClient(t).ContainerStart(ctx, id, container.StartOptions{})
}

func n19Stop(t *testing.T, id string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	to := 5
	return dockerClient(t).ContainerStop(ctx, id, container.StopOptions{Timeout: &to})
}

func n19Restart(t *testing.T, id string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	to := 5
	return dockerClient(t).ContainerRestart(ctx, id, container.StopOptions{Timeout: &to})
}

// n19WaitRunning waits until the container is running and stays so briefly.
func n19WaitRunning(t *testing.T, id string) bool {
	t.Helper()
	dc := dockerClient(t)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		in, err := dc.ContainerInspect(ctx, id)
		cancel()
		if err == nil && in.State != nil && in.State.Running {
			time.Sleep(500 * time.Millisecond)
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func n19Canary(sc string) (value, want string) {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	value = syntheticValue("n19-" + strings.ToLower(sc) + "-" + hex.EncodeToString(b))
	return value, n19Sha(value)
}

// ---- app view ---------------------------------------------------------------

var n19AppRe = regexp.MustCompile(`N19-APP present=([01]) size=(\d+) sha256=(\S*)`)

// n19AppLast returns the last N19-APP line the application logged:
// present, size, sha256, ok.
func n19AppLast(dc *client.Client, id string) (present bool, size, sha string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rc, err := dc.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: false, Tail: "5"})
	if err != nil {
		return false, "", "", false
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, io.Discard, rc); err != nil {
		return false, "", "", false
	}
	var last []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if m := n19AppRe.FindStringSubmatch(l); m != nil {
			last = m
		}
	}
	if last == nil {
		return false, "", "", false
	}
	return last[1] == "1", last[2], last[3], true
}

// n19WaitAppLine waits for the application's first N19-APP line.
func n19WaitAppLine(t *testing.T, id string) (present bool, size, sha string, ok bool) {
	t.Helper()
	dc := dockerClient(t)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if p, s, h, k := n19AppLast(dc, id); k {
			return p, s, h, true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false, "", "", false
}

// ---- raw docker CLI with stdin/stdout (guarded endpoint, DOCKER_* scrubbed) ----

func n19Docker(t *testing.T, stdin []byte, args ...string) (stdout []byte, stderr string, err error) {
	t.Helper()
	sess := dockerguard.Current(t)
	cmd := exec.Command("docker", append([]string{"--host", sess.Config.Host}, args...)...) // #nosec G204 -- fixed binary; endpoint verified by the guard
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DOCKER_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "DOCKER_HOST="+sess.Config.Host)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	return so.Bytes(), strings.TrimSpace(se.String()), err
}

// n19SeedRunning writes value into path of a RUNNING container through the exec
// stdin (never argv), as root with umask 077: how the initial delivery would.
func n19SeedRunning(t *testing.T, id, value string) error {
	t.Helper()
	_, se, err := n19Docker(t, []byte(value), "exec", "-i", id, "sh", "-c", "umask 077; cat > "+n19File)
	if err != nil {
		return fmt.Errorf("seed: %v %s", err, n19Clip(injRedact(se), 160))
	}
	return nil
}

// n19Tar builds the same shape of archive production builds (absolute header name).
func n19Tar(name, content string, uid int) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o400, Size: int64(len(content)), Uid: uid, Gid: uid})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	return buf.Bytes()
}

// n19TarFileHash returns the sha256 of the first regular file in a tar stream.
func n19TarFileHash(b []byte) (string, bool) {
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		hdr, err := tr.Next()
		if err != nil {
			return "", false
		}
		if hdr.Typeflag == tar.TypeReg {
			h := sha256.New()
			_, _ = io.Copy(h, io.LimitReader(tr, 1<<20))
			return hex.EncodeToString(h.Sum(nil)), true
		}
	}
}
