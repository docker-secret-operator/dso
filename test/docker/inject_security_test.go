//go:build dockerint

package docker

// `dso inject` security scenarios (docs/audit/38-DSO-INJECT-SECURITY-FINDING.md).
//
// CONTRACT under test (audit 38 section 2):
//   - `dso inject` is an explicit ADMINISTRATIVE command: the operator names the
//     target container; DSO ownership labels are NOT required (a managed and an
//     unmanaged container are treated alike).
//   - It writes ONLY into a directory that lies inside a tmpfs mount declared by
//     a RUNNING target, delivered by the exec injector (atomic rename, 0400,
//     root-owned). Anything else (no tmpfs, bind, volume, stopped container,
//     symlinked directory, unsafe path) is REFUSED and leaves nothing behind.
//   - The secret never appears in command output, docker logs or docker inspect.
//
// Every test is its own top-level test so scripts/integration/run-all.sh can
// require each by name. Synthetic secrets only (sentinel + version).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

const injTmpfs = injDir + ":rw,mode=0755"

func TestInjectSecurity_01_ManagedContainerVerifiedTmpfs(t *testing.T) {
	_, name := injStart(t, injSpec{
		labels: map[string]string{"dso.reloader": "true", "dso.owner": ownerID, "dso.secrets": secretName},
		tmpfs:  []string{injTmpfs},
	})
	val := syntheticValue("inj01")
	out, err := injRun(t, val, "--container", name, "--secret", secretName)
	if err != nil {
		t.Fatalf("inject into managed container with tmpfs failed: %v\n%s", err, injRedact(out))
	}
	file := injDir + "/" + secretName
	injAssertContent(t, name, file, val)
	if got := injFSType(t, name, injDir); got != "tmpfs" {
		t.Fatalf("destination filesystem = %q, want tmpfs", got)
	}
}

func TestInjectSecurity_02_UnmanagedContainerExplicitTarget(t *testing.T) {
	// Model C (administrative): no dso.* labels, explicit --container, declared tmpfs.
	_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	val := syntheticValue("inj02")
	out, err := injRun(t, val, "--container", name, "--secret", secretName)
	if err != nil {
		t.Fatalf("explicit inject into an unmanaged container with tmpfs failed: %v\n%s", err, injRedact(out))
	}
	injAssertContent(t, name, injDir+"/"+secretName, val)
}

func TestInjectSecurity_03_MissingDestination(t *testing.T) {
	t.Run("no-tmpfs-directory-absent-refused-and-not-created", func(t *testing.T) {
		id, name := injStart(t, injSpec{})
		injFileMissing(t, name, injDir)
		out, err := injRun(t, syntheticValue("inj03a"), "--container", name, "--secret", secretName)
		if err == nil {
			t.Fatalf("inject must refuse when the destination is not on a tmpfs\n%s", injRedact(out))
		}
		injFileMissing(t, name, injDir) // the command must not create the directory either
		injAssertNoDiffMention(t, id, secretName)
	})
	t.Run("subdirectory-inside-declared-tmpfs-is-created-in-tmpfs", func(t *testing.T) {
		_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
		sub := injDir + "/nested"
		val := syntheticValue("inj03b")
		if out, err := injRun(t, val, "--container", name, "--secret", secretName, "--mount", sub); err != nil {
			t.Fatalf("inject into a not-yet-existing subdirectory of a tmpfs failed: %v\n%s", err, injRedact(out))
		}
		injAssertContent(t, name, sub+"/"+secretName, val)
		if got := injFSType(t, name, sub); got != "tmpfs" {
			t.Fatalf("subdirectory filesystem = %q, want tmpfs", got)
		}
	})
}

func TestInjectSecurity_04_WritableLayerDestinationRefused(t *testing.T) {
	id, name := injStart(t, injSpec{})
	// /tmp exists in the image layer: the classic "silently lands on host disk" case.
	for _, mount := range []string{"/tmp", "/etc", injDir} {
		val := syntheticValue("inj04")
		out, err := injRun(t, val, "--container", name, "--secret", secretName, "--mount", mount)
		if err == nil {
			t.Fatalf("inject to writable-layer directory %s must be refused\n%s", mount, injRedact(out))
		}
		injFileMissing(t, name, mount+"/"+secretName)
	}
	injAssertNoDiffMention(t, id, secretName)
}

func TestInjectSecurity_05_BindMountDestinationRefused(t *testing.T) {
	host := t.TempDir()
	_, name := injStart(t, injSpec{mounts: []string{host + ":" + injDir}})
	out, err := injRun(t, syntheticValue("inj05"), "--container", name, "--secret", secretName)
	if err == nil {
		t.Fatalf("inject onto a bind mount must be refused\n%s", injRedact(out))
	}
	entries, rerr := os.ReadDir(host)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(entries) != 0 {
		t.Fatalf("host bind source was written to: %d entries", len(entries))
	}
}

func TestInjectSecurity_06_NamedVolumeDestinationRefused(t *testing.T) {
	vol := injVolume(t)
	_, name := injStart(t, injSpec{mounts: []string{vol + ":" + injDir}})
	out, err := injRun(t, syntheticValue("inj06"), "--container", name, "--secret", secretName)
	if err == nil {
		t.Fatalf("inject onto a named volume must be refused\n%s", injRedact(out))
	}
	if left := injExecOK(t, name, "sh", "-c", "ls -A "+injDir+" | wc -l"); strings.TrimSpace(left) != "0" {
		t.Fatalf("named volume was written to (%s entries)", left)
	}
}

func TestInjectSecurity_07_StoppedContainerRefused(t *testing.T) {
	id, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	dockerCmd(t, "stop", "-t", "2", id)
	out, err := injRun(t, syntheticValue("inj07"), "--container", name, "--secret", secretName)
	if err == nil || !strings.Contains(out, "not running") {
		t.Fatalf("stopped container must be refused with a not-running error, got err=%v\n%s", err, injRedact(out))
	}
	injAssertNoDiffMention(t, id, secretName)
}

func TestInjectSecurity_08_WrongContainerIdentity(t *testing.T) {
	t.Run("unknown-reference", func(t *testing.T) {
		out, err := injRun(t, syntheticValue("inj08a"), "--container", uniqueName(t, "dso-it-nonexistent"), "--secret", secretName)
		if err == nil {
			t.Fatalf("unknown container must be an error\n%s", injRedact(out))
		}
	})
	t.Run("removed-container-id-does-not-reach-a-same-named-successor", func(t *testing.T) {
		oldID, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
		dockerCmd(t, "rm", "-f", oldID)
		// A new container takes the name; the stale full ID must not resolve to it.
		args := append([]string{"run", "-d", "--name", name}, injLabelArgs(t)...)
		args = append(args, "--tmpfs", injTmpfs, testImage, "sh", "-c", injIdleCmd)
		newID := dockerCmd(t, args...)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = dockerClient(t).ContainerRemove(ctx, newID, container.RemoveOptions{Force: true})
		})
		out, err := injRun(t, syntheticValue("inj08b"), "--container", oldID, "--secret", secretName)
		if err == nil {
			t.Fatalf("stale container ID must not resolve\n%s", injRedact(out))
		}
		injFileMissing(t, name, injDir+"/"+secretName)
	})
	t.Run("only-the-named-container-is-modified", func(t *testing.T) {
		_, a := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
		_, b := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
		if out, err := injRun(t, syntheticValue("inj08c"), "--container", a, "--secret", secretName); err != nil {
			t.Fatalf("inject: %v\n%s", err, injRedact(out))
		}
		injFileMissing(t, b, injDir+"/"+secretName)
	})
}

func TestInjectSecurity_09_UnrelatedWorkload(t *testing.T) {
	t.Run("unrelated-workload-without-tmpfs-is-refused-and-untouched", func(t *testing.T) {
		id, name := injStart(t, injSpec{labels: map[string]string{"dso.reloader": "true", "dso.owner": "some-other-agent"}})
		out, err := injRun(t, syntheticValue("inj09a"), "--container", name, "--secret", secretName)
		if err == nil {
			t.Fatalf("workload without a tmpfs destination must be refused\n%s", injRedact(out))
		}
		injAssertNoDiffMention(t, id, secretName)
	})
	t.Run("foreign-owner-label-is-not-an-authorization-gate-for-an-explicit-target", func(t *testing.T) {
		// Documented contract (audit 38): ownership labels govern the agent's
		// automatic actions; an operator who names a container and has Docker
		// access may inject. The destination still has to be a declared tmpfs.
		_, name := injStart(t, injSpec{
			labels: map[string]string{"dso.reloader": "true", "dso.owner": "some-other-agent"},
			tmpfs:  []string{injTmpfs},
		})
		val := syntheticValue("inj09b")
		if out, err := injRun(t, val, "--container", name, "--secret", secretName); err != nil {
			t.Fatalf("explicit inject into foreign-owner container with tmpfs: %v\n%s", err, injRedact(out))
		}
		injAssertContent(t, name, injDir+"/"+secretName, val)
	})
}

func TestInjectSecurity_10_SymlinkDestination(t *testing.T) {
	t.Run("destination-directory-is-a-symlink-refused", func(t *testing.T) {
		_, name := injStart(t, injSpec{tmpfs: []string{"/data:rw,mode=0755"}})
		injExecOK(t, name, "sh", "-c", "mkdir /data/real && ln -s /data/real /data/link")
		out, err := injRun(t, syntheticValue("inj10a"), "--container", name, "--secret", secretName, "--mount", "/data/link")
		if err == nil {
			t.Fatalf("symlinked destination directory must be refused\n%s", injRedact(out))
		}
		injFileMissing(t, name, "/data/real/"+secretName)
	})
	t.Run("destination-file-is-a-symlink-is-replaced-not-followed", func(t *testing.T) {
		_, name := injStart(t, injSpec{tmpfs: []string{"/data:rw,mode=0755"}})
		injExecOK(t, name, "sh", "-c", "echo victim-original > /data/victim && ln -s /data/victim /data/"+secretName)
		val := syntheticValue("inj10b")
		if out, err := injRun(t, val, "--container", name, "--secret", secretName, "--mount", "/data"); err != nil {
			t.Fatalf("inject: %v\n%s", err, injRedact(out))
		}
		if got := injExecOK(t, name, "cat", "/data/victim"); got != "victim-original" {
			t.Fatalf("symlink target was overwritten through the link")
		}
		if out := injExecOK(t, name, "sh", "-c", "test -L /data/"+secretName+" && echo link || echo regular"); out != "regular" {
			t.Fatalf("destination is still a symlink")
		}
		injAssertContent(t, name, "/data/"+secretName, val)
	})
}

func TestInjectSecurity_11_UnsafeDestinationPath(t *testing.T) {
	id, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	cases := []struct{ label, mount, secret string }{
		{"dotdot-in-mount", injDir + "/../../etc", secretName},
		{"relative-mount", "run/secrets/dso", secretName},
		{"double-slash-mount", "//run/secrets/dso", secretName},
		{"trailing-slash-mount", injDir + "/", secretName},
		{"root-mount", "/", secretName},
		{"dotdot-secret-name", injDir, "../escape"},
		{"slash-in-secret-name", injDir, "sub/dir"},
		{"shell-metachar-secret-name", injDir, "a;touch pwned"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			out, err := injRun(t, syntheticValue("inj11"), "--container", name, "--secret", c.secret, "--mount", c.mount)
			if err == nil {
				t.Fatalf("unsafe destination %q %q must be refused\n%s", c.mount, c.secret, injRedact(out))
			}
		})
	}
	injFileMissing(t, name, "/etc/"+secretName)
	injFileMissing(t, name, "/pwned")
	injFileMissing(t, name, injDir+"/escape")
	injAssertNoDiffMention(t, id, secretName, "escape", "pwned")
}

func TestInjectSecurity_12_NonRootFileAccess(t *testing.T) {
	// Designed behaviour: the file is root-owned 0400 (inject has no uid/gid
	// option; delivery with an application uid/gid is the agent's managed path),
	// so a NON-root process in the container must NOT be able to read it, and
	// root (the injector's identity) can.
	_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	val := syntheticValue("inj12")
	if out, err := injRun(t, val, "--container", name, "--secret", secretName); err != nil {
		t.Fatalf("inject: %v\n%s", err, injRedact(out))
	}
	file := injDir + "/" + secretName
	if owner := injExecOK(t, name, "stat", "-c", "%u:%g", file); owner != "0:0" {
		t.Fatalf("owner = %s, want 0:0", owner)
	}
	injAssertContent(t, name, file, val)
	if out, err := injExecAs(t, name, "10001:10001", "cat", file); err == nil {
		t.Fatalf("a non-root user read the root-owned 0400 secret (%d bytes): permissions are too permissive", len(out))
	}
}

func TestInjectSecurity_13_RestrictivePermissions(t *testing.T) {
	_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	if out, err := injRun(t, syntheticValue("inj13"), "--container", name, "--secret", secretName); err != nil {
		t.Fatalf("inject: %v\n%s", err, injRedact(out))
	}
	file := injDir + "/" + secretName
	if mode := injExecOK(t, name, "stat", "-c", "%a", file); mode != "400" {
		t.Fatalf("mode = %s, want 400 (no group/other access)", mode)
	}
	if typ := injExecOK(t, name, "stat", "-c", "%F", file); typ != "regular file" {
		t.Fatalf("type = %q", typ)
	}
	// No temporary files left behind by the atomic publish.
	if left := injExecOK(t, name, "sh", "-c", "ls -A "+injDir+" | grep -c '^\\.inject\\.' || true"); strings.TrimSpace(left) != "0" {
		t.Fatalf("%s temporary file(s) left behind", left)
	}
}

func TestInjectSecurity_14_PartialInjectionFailure(t *testing.T) {
	// A tiny tmpfs makes the write fail midway (ENOSPC). The previous complete
	// file must survive and no temporary file may remain.
	_, name := injStart(t, injSpec{tmpfs: []string{injDir + ":rw,size=16k,mode=0755"}})
	v1 := syntheticValue("inj14-v1")
	if out, err := injRun(t, v1, "--container", name, "--secret", secretName); err != nil {
		t.Fatalf("first inject: %v\n%s", err, injRedact(out))
	}
	big := strings.Repeat(syntheticValue("inj14-big")+"-", 4096) // ~100 KB > 16 KiB tmpfs
	out, err := injRun(t, big, "--container", name, "--secret", secretName)
	if err == nil {
		t.Fatalf("oversized inject into a 16 KiB tmpfs must fail\n%s", injRedact(out))
	}
	injNoLeak(t, "failure output", out)
	injAssertContent(t, name, injDir+"/"+secretName, v1)
	if left := injExecOK(t, name, "sh", "-c", "ls -A "+injDir+" | grep -c '^\\.inject\\.' || true"); strings.TrimSpace(left) != "0" {
		t.Fatalf("%s temporary file(s) left behind after failure", left)
	}
}

func TestInjectSecurity_15_RepeatedInjection(t *testing.T) {
	_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	file := injDir + "/" + secretName
	for i, v := range []string{"v1", "v2", "v2", "v3"} {
		val := syntheticValue("inj15-" + v)
		if out, err := injRun(t, val, "--container", name, "--secret", secretName); err != nil {
			t.Fatalf("inject #%d: %v\n%s", i+1, err, injRedact(out))
		}
		injAssertContent(t, name, file, val)
		if mode := injExecOK(t, name, "stat", "-c", "%a", file); mode != "400" {
			t.Fatalf("inject #%d: mode = %s", i+1, mode)
		}
	}
	if n := injExecOK(t, name, "sh", "-c", "ls -A "+injDir+" | wc -l"); strings.TrimSpace(n) != "1" {
		t.Fatalf("directory holds %s entries after repeated injection, want 1", n)
	}
}

func TestInjectSecurity_16_NoPlaintextInPersistentLocations(t *testing.T) {
	host := t.TempDir()
	vol := injVolume(t)
	id, name := injStart(t, injSpec{
		tmpfs:  []string{injTmpfs},
		mounts: []string{host + ":/mnt/bind", vol + ":/mnt/vol"},
	})
	val := syntheticValue("inj16")
	if out, err := injRun(t, val, "--container", name, "--secret", secretName); err != nil {
		t.Fatalf("inject: %v\n%s", err, injRedact(out))
	}
	injAssertNoDiffMention(t, id, secretName)
	// Sentinel scan over the container's own filesystem (-xdev: excludes the
	// tmpfs destination, /proc, /sys, /dev) and both persistent mounts. A
	// positive control proves the scan can find a planted copy first.
	const scan = `{ find / -xdev -type f; find /mnt/bind /mnt/vol -type f; } 2>/dev/null | xargs -r grep -lF -- "$S" 2>/dev/null; true`
	if o, perr := injExecEnv(t, name, "S="+sentinel, "sh", "-c", `printf '%s' "$S" > /mnt/vol/scan-control`); perr != nil {
		t.Fatalf("plant scan control: %v %s", perr, injRedact(o))
	}
	ctl, err := injExecEnv(t, name, "S="+sentinel, "sh", "-c", scan)
	if err != nil || !strings.Contains(ctl, "/mnt/vol/scan-control") {
		t.Fatalf("scan positive control failed (err=%v): the scan is not trustworthy", err)
	}
	injExecOK(t, name, "rm", "-f", "/mnt/vol/scan-control")
	out, err := injExecEnv(t, name, "S="+sentinel, "sh", "-c", scan)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("synthetic secret found in persistent locations: %s", injRedact(out))
	}
	// Host side: the bind source holds nothing; the layer's upper dir is scanned when readable.
	if entries, _ := os.ReadDir(host); len(entries) != 0 {
		t.Fatalf("bind source modified")
	}
	in, ierr := dockerClient(t).ContainerInspect(context.Background(), id)
	if ierr != nil {
		t.Fatal(ierr)
	}
	if upper := in.GraphDriver.Data["UpperDir"]; upper != "" {
		leak := false
		walkErr := filepath.Walk(upper, func(p string, fi os.FileInfo, err error) error {
			if err != nil || !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
				return nil
			}
			if b, rerr := os.ReadFile(p); rerr == nil && strings.Contains(string(b), sentinel) { // #nosec G304 -- daemon-owned path of this run's container
				leak = true
			}
			return nil
		})
		if leak {
			t.Fatalf("synthetic secret found in the container's layer on the host")
		}
		if walkErr != nil {
			t.Logf("NOTE: layer UpperDir not fully readable by the test user (%v); the docker-diff and in-container scans above stand", walkErr)
		}
	} else {
		t.Log("NOTE: storage driver exposes no UpperDir; host-side layer scan not possible, docker diff and in-container scans used")
	}
}

func TestInjectSecurity_17_NoSecretInLogsOrMetadata(t *testing.T) {
	id, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	val := syntheticValue("inj17")
	okOut, err := injRun(t, val, "--container", name, "--secret", secretName)
	if err != nil {
		t.Fatalf("inject: %v\n%s", err, injRedact(okOut))
	}
	injNoLeak(t, "successful command output", okOut)
	// A refused run must not echo the value either.
	badOut, berr := injRun(t, val, "--container", name, "--secret", secretName, "--mount", "/tmp")
	if berr == nil {
		t.Fatal("expected refusal for /tmp")
	}
	injNoLeak(t, "refusal output", badOut)
	// Container logs and inspect metadata.
	injNoLeak(t, "docker logs", dockerCmd(t, "logs", id))
	injNoLeak(t, "docker inspect", dockerCmd(t, "inspect", id))
	// The exec the injector ran must not expose the value in argv/env of anything still running.
	procs := injExecOK(t, name, "sh", "-c", "cat /proc/[0-9]*/cmdline 2>/dev/null | tr '\\0' ' ' || true")
	injNoLeak(t, "process command lines in the container", procs)
}

func TestInjectSecurity_18_PatchedEngineNormalBehaviour(t *testing.T) {
	sv, err := dockerClient(t).ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("server version: %v", err)
	}
	t.Logf("docker server version: %s (api %s, os/arch %s/%s)", sv.Version, sv.APIVersion, sv.Os, sv.Arch)
	v := injSemver(sv.Version)
	switch {
	case v[0] >= 29 && injAtLeast(v, [3]int{29, 5, 1}):
		t.Logf("Engine %s is at or above the docker-cp fixed boundary 29.5.1", sv.Version)
	case v[0] == 25 && injAtLeast(v, [3]int{25, 0, 17}):
		t.Logf("Engine %s is at or above the docker-cp fixed boundary 25.0.17", sv.Version)
	default:
		t.Logf("NOTE: Engine %s is BELOW the fixed boundaries (29.5.1 / 25.0.17) for CVE-2026-42306, CVE-2026-41568, CVE-2026-41567; running the normal scenario anyway (compatibility evidence only, not a security statement)", sv.Version)
	}
	if want := os.Getenv("DSO_IT_EXPECT_DOCKER_VERSION"); want != "" && !strings.HasPrefix(sv.Version, strings.TrimPrefix(want, "v")) {
		t.Fatalf("Engine %s does not match DSO_IT_EXPECT_DOCKER_VERSION=%s", sv.Version, want)
	}
	_, name := injStart(t, injSpec{tmpfs: []string{injTmpfs}})
	val := syntheticValue("inj18")
	started := time.Now()
	if out, err := injRun(t, val, "--container", name, "--secret", secretName); err != nil {
		t.Fatalf("normal inject failed on Engine %s: %v\n%s", sv.Version, err, injRedact(out))
	}
	injAssertContent(t, name, injDir+"/"+secretName, val)
	t.Log(fmt.Sprintf("normal inject on Engine %s completed in %s", sv.Version, time.Since(started).Round(time.Millisecond)))
}
