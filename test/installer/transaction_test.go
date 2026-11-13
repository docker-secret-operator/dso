package installer

// [M0-05] Installation-consistency tests: the previous working installation (core
// binary + provider plugins + dso symlink) must be restored byte-for-byte when the
// installer fails at ANY stage after (or before) verification, or is interrupted.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// snapshot records every entry under root: type, permission bits, content hash or
// symlink target. Two snapshots are equal only if the trees are byte-identical.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			tg, _ := os.Readlink(p)
			lines = append(lines, fmt.Sprintf("L %s -> %s", rel, tg))
		case fi.IsDir():
			lines = append(lines, fmt.Sprintf("D %s %o", rel, fi.Mode().Perm()))
		default:
			b, _ := os.ReadFile(p)
			h := sha256.Sum256(b)
			lines = append(lines, fmt.Sprintf("F %s %o %s", rel, fi.Mode().Perm(), hex.EncodeToString(h[:8])))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// seedOldInstall lays down a previous working installation.
func (f *fixture) seedOldInstall() {
	f.t.Helper()
	w := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(f.home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			f.t.Fatal(err)
		}
	}
	w(".docker/cli-plugins/docker-dso", "OLD-CORE-BINARY", 0o755)
	w(".dso/plugins/dso-provider-test", "OLD-PROVIDER", 0o755)
	w(".dso/plugins/dso-provider-other", "OLD-OTHER-PROVIDER", 0o755)
	if err := os.MkdirAll(filepath.Join(f.home, ".local/bin"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.home, ".docker/cli-plugins/docker-dso"), filepath.Join(f.home, ".local/bin/dso")); err != nil {
		f.t.Fatal(err)
	}
}

func TestInstaller_FailureAtEveryStageRestoresThePreviousInstallation(t *testing.T) {
	points := []struct {
		fp       string
		restored bool // something had already been published, so a rollback message is expected
	}{
		{"stage_provider", false}, {"stage_core", false}, {"stage_link", false},
		{"publish_provider", false}, {"publish_core", true}, {"publish_link", true},
		{"after_providers", true}, {"after_core", true}, {"postcheck", true},
	}
	for _, withOld := range []bool{true, false} {
		for _, p := range points {
			name := fmt.Sprintf("%s/old=%v", p.fp, withOld)
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				f.publish(f.key)
				if withOld {
					f.seedOldInstall()
				}
				before := snapshot(t, f.home)
				r := f.install(map[string]string{"DSO_INSTALL_TEST_FAILPOINT": p.fp}, "")
				if r.code == 0 {
					t.Fatalf("injected failure at %s must fail the install:\n%s", p.fp, r.out)
				}
				if !strings.Contains(r.out, "injected failure at "+p.fp) {
					t.Fatalf("the failure was not the injected one:\n%s", r.out)
				}
				if after := snapshot(t, f.home); after != before {
					t.Fatalf("installation changed after a failure at %s.\nBEFORE:\n%s\nAFTER:\n%s\noutput:\n%s", p.fp, before, after, r.out)
				}
				if withOld && p.restored && !strings.Contains(r.out, "previous installation was restored") {
					t.Fatalf("expected a rollback message for %s:\n%s", p.fp, r.out)
				}
			})
		}
	}
}

// Guard against the failure tests passing vacuously: without an injected failure the
// same fixture really replaces core, provider and symlink.
func TestInstaller_SuccessfulUpgradeReplacesAllPieces(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	f.seedOldInstall()
	before := snapshot(t, f.home)
	r := f.install(nil, "")
	if r.code != 0 {
		t.Fatalf("upgrade failed:\n%s", r.out)
	}
	if snapshot(t, f.home) == before {
		t.Fatal("a successful upgrade changed nothing")
	}
	if b, _ := os.ReadFile(f.installed()); !strings.Contains(string(b), "new-binary") {
		t.Fatalf("core not upgraded: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(f.home, ".dso/plugins/dso-provider-test")); !strings.Contains(string(b), "provider") || strings.Contains(string(b), "OLD") {
		t.Fatalf("provider not upgraded: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(f.home, ".dso/plugins/dso-provider-other")); string(b) != "OLD-OTHER-PROVIDER" {
		t.Fatalf("an unrelated provider must be untouched: %q", b)
	}
	tg, err := os.Readlink(filepath.Join(f.home, ".local/bin/dso"))
	if err != nil || tg != f.installed() {
		t.Fatalf("symlink wrong: %q %v", tg, err)
	}
	// no staging/backup leftovers anywhere
	_ = filepath.WalkDir(f.home, func(p string, d fs.DirEntry, _ error) error {
		if strings.Contains(d.Name(), ".dso-stage") || strings.Contains(d.Name(), ".dso-bak") || strings.Contains(d.Name(), ".dso-rb") {
			t.Fatalf("leftover temp file %s", p)
		}
		return nil
	})
}

// Verification failures (before anything is staged) leave the whole previous
// installation byte-identical.
func TestInstaller_VerificationFailureLeavesPreviousInstallationByteIdentical(t *testing.T) {
	cases := map[string]func(f *fixture){
		"tampered archive": func(f *fixture) { f.write(f.tarName, buildTarball(t, "#!/bin/sh\necho EVIL\n")) },
		"signature by wrong key": func(f *fixture) {
			dir := t.TempDir()
			cmd := exec.Command(f.cosign, "generate-key-pair")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			f.sign(filepath.Join(dir, "cosign.key"))
		},
		"missing bundle": func(f *fixture) {
			_ = os.Remove(f.path(fmt.Sprintf("dso-%s-checksums.txt.sigstore.json", version)))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.publish(f.key)
			f.seedOldInstall()
			mutate(f)
			before := snapshot(t, f.home)
			if r := f.install(nil, ""); r.code == 0 {
				t.Fatalf("must fail:\n%s", r.out)
			}
			if after := snapshot(t, f.home); after != before {
				t.Fatalf("previous installation changed.\nBEFORE:\n%s\nAFTER:\n%s", before, after)
			}
		})
	}
}

// A real write failure while staging (provider directory not writable) must leave the
// core binary, the other providers and the symlink untouched.
func TestInstaller_UnwritableProviderDirectoryLeavesPreviousInstallation(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	f.seedOldInstall()
	pd := filepath.Join(f.home, ".dso/plugins")
	if err := os.Chmod(pd, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(pd, 0o755) }()
	before := snapshot(t, f.home)
	if r := f.install(nil, ""); r.code == 0 {
		t.Fatalf("must fail:\n%s", r.out)
	}
	if after := snapshot(t, f.home); after != before {
		t.Fatalf("changed.\nBEFORE:\n%s\nAFTER:\n%s", before, after)
	}
}

// Interruption: the installer is paused mid-publish (after the new providers and/or
// the new core binary are live), then signalled. The previous installation must be
// restored.
func TestInstaller_InterruptedMidInstallRestoresThePreviousInstallation(t *testing.T) {
	for _, pause := range []string{"after_providers", "after_core"} {
		for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
			t.Run(fmt.Sprintf("%s/%v", pause, sig), func(t *testing.T) {
				f := newFixture(t)
				f.publish(f.key)
				f.seedOldInstall()
				before := snapshot(t, f.home)

				cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts/install.sh"))
				cmd.Env = []string{
					"HOME=" + f.home, "PATH=" + stubBin(t, "") + ":" + os.Getenv("PATH"),
					"DSO_VERSION=" + version, "DSO_RELEASE_BASE=" + f.base, "DSO_COSIGN_BIN=" + f.cosign,
					"DSO_COSIGN_PUBKEY=" + f.pub, "DSO_INSTALL_TEST_FAILPOINT=pause:" + pause,
				}
				stdout, _ := cmd.StdoutPipe()
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				paused := make(chan string, 1)
				var out bytes.Buffer
				go func() {
					sc := bufio.NewScanner(stdout)
					for sc.Scan() {
						out.WriteString(sc.Text() + "\n")
						if strings.Contains(sc.Text(), "DSO_INSTALL_PAUSED") {
							paused <- sc.Text()
							_, _ = io.Copy(io.Discard, stdout)
							return
						}
					}
					close(paused)
				}()
				select {
				case line, ok := <-paused:
					if !ok {
						_ = cmd.Process.Kill()
						t.Fatalf("installer exited before pausing:\n%s\n%s", out.String(), stderr.String())
					}
					_ = line
				case <-time.After(60 * time.Second):
					_ = cmd.Process.Kill()
					t.Fatal("timeout waiting for the installer to pause")
				}
				// Prove the test is meaningful: the install is half-published right now.
				if mid := snapshot(t, f.home); mid == before {
					_ = cmd.Process.Kill()
					t.Fatal("nothing was published at the pause point; the interruption test would be vacuous")
				}
				if pause == "after_core" {
					if b, _ := os.ReadFile(f.installed()); !strings.Contains(string(b), "new-binary") {
						_ = cmd.Process.Kill()
						t.Fatalf("core binary was not yet replaced at the pause point: %q", b)
					}
				}
				_ = cmd.Process.Signal(sig)
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					_ = cmd.Process.Kill()
					t.Fatal("installer did not exit after the signal")
				}
				if cmd.ProcessState.ExitCode() == 0 {
					t.Fatal("an interrupted install must not exit 0")
				}
				if after := snapshot(t, f.home); after != before {
					t.Fatalf("interrupted install left a mixed state.\nBEFORE:\n%s\nAFTER:\n%s\nstderr:\n%s", before, after, stderr.String())
				}
			})
		}
	}
}
