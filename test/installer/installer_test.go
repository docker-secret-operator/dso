// Package installer tests scripts/install.sh: the release must be authenticated
// (signature on the checksum manifest, checked BEFORE any hash in it is trusted)
// before anything is installed, and the installer must fail closed. Real cosign
// cryptography is used in key mode; the keyless identity/issuer arguments (which
// cannot be exercised without a real GitHub OIDC signature) are verified through a
// recording cosign shim. No network access and no Docker daemon are used.
package installer

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const version = "3.9.9"

func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func needCosign(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("DSO_TEST_COSIGN"); p != "" {
		return p
	}
	p, err := exec.LookPath("cosign")
	if err != nil {
		t.Skip("cosign is not installed: the real-signature installer tests did not run (install a verified cosign and set DSO_TEST_COSIGN)")
	}
	return p
}

func platform(t *testing.T) (goos, goarch string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("installer tests assume a non-root user (user-local install paths)")
	}
	goos = runtime.GOOS
	switch runtime.GOARCH {
	case "amd64", "arm64":
		goarch = runtime.GOARCH
	default:
		t.Skip("unsupported test architecture")
	}
	return
}

type fixture struct {
	t        *testing.T
	root     string // release files live in root/v<version>/
	home     string
	base     string
	cosign   string
	pub, key string
	tarName  string
	tarBytes []byte
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func buildTarball(t *testing.T, binContent string, extra ...[2]string) []byte {
	t.Helper()
	var buf strings.Builder
	_ = buf
	f, err := os.CreateTemp(t.TempDir(), "t*.tgz")
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	add := func(name, content string, mode int64) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(content))
	}
	add("docker-dso", binContent, 0o755)
	add("dso-provider-test", "#!/bin/sh\necho provider\n", 0o755)
	for _, e := range extra {
		add(e[0], e[1], 0o755)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	b, _ := os.ReadFile(f.Name())
	return b
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	goos, goarch := platform(t)
	cosign := needCosign(t)
	root := t.TempDir()
	f := &fixture{t: t, root: root, home: t.TempDir(), cosign: cosign,
		base:    "file://" + root,
		tarName: fmt.Sprintf("dso-%s-%s-%s.tar.gz", version, goos, goarch)}
	if err := os.MkdirAll(filepath.Join(root, "v"+version), 0o755); err != nil {
		t.Fatal(err)
	}
	keyDir := t.TempDir()
	cmd := exec.Command(cosign, "generate-key-pair")
	cmd.Dir = keyDir
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate-key-pair: %v\n%s", err, out)
	}
	f.key, f.pub = filepath.Join(keyDir, "cosign.key"), filepath.Join(keyDir, "cosign.pub")
	f.tarBytes = buildTarball(t, "#!/bin/sh\necho new-binary\n")
	return f
}

func (f *fixture) path(name string) string { return filepath.Join(f.root, "v"+version, name) }
func (f *fixture) write(name string, b []byte) {
	f.t.Helper()
	if err := os.WriteFile(f.path(name), b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// publish writes the tarball, a manifest (with a distractor line first) and a bundle
// signed by key.
func (f *fixture) publish(signKey string) {
	f.t.Helper()
	f.write(f.tarName, f.tarBytes)
	manifest := fmt.Sprintf("%s  %s.sbom.json\n%s  %s\n%s  other-file.txt\n",
		strings.Repeat("0", 64), f.tarName, sha(f.tarBytes), f.tarName, strings.Repeat("1", 64))
	f.write(fmt.Sprintf("dso-%s-checksums.txt", version), []byte(manifest))
	f.sign(signKey)
}

func (f *fixture) sign(key string) {
	f.t.Helper()
	sc := filepath.Join(f.t.TempDir(), "sc.json")
	_ = os.WriteFile(sc, []byte(`{"mediaType":"application/vnd.dev.sigstore.signingconfig.v0.2+json","caUrls":[],"oidcUrls":[],"rekorTlogUrls":[],"tsaUrls":[],"rekorTlogConfig":{"selector":"ANY"},"tsaConfig":{"selector":"ANY"}}`), 0o644)
	manifest := f.path(fmt.Sprintf("dso-%s-checksums.txt", version))
	cmd := exec.Command(f.cosign, "sign-blob", "--key", key, "--yes", "--signing-config", sc, "--bundle", manifest+".sigstore.json", manifest)
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("sign-blob: %v\n%s", err, out)
	}
}

// stubBin returns a PATH-first directory with a failing `docker` (so the installer's
// daemon check can never reach a real daemon) and an optional uname override.
func stubBin(t *testing.T, uname string) string {
	t.Helper()
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	if uname != "" {
		_ = os.WriteFile(filepath.Join(d, "uname"), []byte(uname), 0o755)
	}
	return d
}

type result struct {
	out  string
	code int
}

func (f *fixture) install(env map[string]string, uname string) result {
	f.t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot(f.t), "scripts/install.sh"))
	e := []string{
		"HOME=" + f.home,
		"PATH=" + stubBin(f.t, uname) + ":" + os.Getenv("PATH"),
		"DSO_VERSION=" + version,
		"DSO_RELEASE_BASE=" + f.base,
		"DSO_COSIGN_BIN=" + f.cosign,
		"DSO_COSIGN_PUBKEY=" + f.pub,
	}
	for k, v := range env {
		e = append(e, k+"="+v)
	}
	cmd.Env = e
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return result{string(out), code}
}

func (f *fixture) installed() string { return filepath.Join(f.home, ".docker/cli-plugins/docker-dso") }

func (f *fixture) assertNothingInstalled(r result) {
	f.t.Helper()
	if r.code == 0 {
		f.t.Fatalf("the install must fail closed, output:\n%s", r.out)
	}
	for _, p := range []string{f.installed(), filepath.Join(f.home, ".dso/plugins/dso-provider-test"), filepath.Join(f.home, ".local/bin/dso")} {
		if _, err := os.Lstat(p); err == nil {
			f.t.Fatalf("%s exists after a failed install", p)
		}
	}
}

func TestInstaller_ValidSignedReleaseInstallsAtomically(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	r := f.install(nil, "")
	if r.code != 0 {
		t.Fatalf("valid release must install (exit %d):\n%s", r.code, r.out)
	}
	b, err := os.ReadFile(f.installed())
	if err != nil || !strings.Contains(string(b), "new-binary") {
		t.Fatalf("binary not installed: %v %q", err, b)
	}
	if fi, _ := os.Stat(f.installed()); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(f.home, ".dso/plugins/dso-provider-test")); err != nil {
		t.Fatalf("provider plugin from the verified archive not installed: %v", err)
	}
	for _, dir := range []string{filepath.Dir(f.installed()), filepath.Join(f.home, ".dso/plugins")} {
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			if strings.Contains(e.Name(), ".") && len(e.Name()) > 12 && strings.Count(e.Name(), ".") >= 1 && e.Name() != "docker-dso" && !strings.HasPrefix(e.Name(), "dso-provider") {
				t.Fatalf("temporary file left in %s: %s", dir, e.Name())
			}
		}
	}
}

func TestInstaller_FailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fixture)
		env   map[string]string
		want  string
	}{
		{"modified binary after signing", func(f *fixture) {
			f.publish(f.key)
			f.write(f.tarName, buildTarball(t, "#!/bin/sh\necho EVIL\n"))
		}, nil, "Integrity check FAILED"},
		{"truncated (partial) tarball", func(f *fixture) {
			f.publish(f.key)
			f.write(f.tarName, f.tarBytes[:len(f.tarBytes)/2])
		}, nil, "Integrity check FAILED"},
		{"modified checksum manifest", func(f *fixture) {
			f.publish(f.key)
			f.write(fmt.Sprintf("dso-%s-checksums.txt", version), []byte(fmt.Sprintf("%s  %s\n", sha([]byte("evil")), f.tarName)))
		}, nil, "signature verification FAILED"},
		{"manifest re-signed by a different key", func(f *fixture) {
			f.publish(f.key)
			other := t.TempDir()
			cmd := exec.Command(f.cosign, "generate-key-pair")
			cmd.Dir = other
			cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			f.sign(filepath.Join(other, "cosign.key"))
		}, nil, "signature verification FAILED"},
		{"corrupted signature bundle", func(f *fixture) {
			f.publish(f.key)
			f.write(fmt.Sprintf("dso-%s-checksums.txt.sigstore.json", version), []byte("{not a bundle"))
		}, nil, "signature verification FAILED"},
		{"missing signature bundle", func(f *fixture) {
			f.publish(f.key)
			_ = os.Remove(f.path(fmt.Sprintf("dso-%s-checksums.txt.sigstore.json", version)))
		}, nil, "unsigned release"},
		{"manifest without an entry for the artifact", func(f *fixture) {
			f.publish(f.key)
			f.write(fmt.Sprintf("dso-%s-checksums.txt", version), []byte(strings.Repeat("a", 64)+"  something-else.tar.gz\n"))
			f.sign(f.key)
		}, nil, "exactly one entry"},
		{"duplicate manifest entries", func(f *fixture) {
			f.publish(f.key)
			f.write(fmt.Sprintf("dso-%s-checksums.txt", version), []byte(fmt.Sprintf("%s  %s\n%s  %s\n", sha(f.tarBytes), f.tarName, sha(f.tarBytes), f.tarName)))
			f.sign(f.key)
		}, nil, "exactly one entry"},
		{"requested version that is not published", func(f *fixture) { f.publish(f.key) }, map[string]string{"DSO_VERSION": "9.9.9"}, "failed to download"},
		{"malformed version / path injection", func(f *fixture) { f.publish(f.key) }, map[string]string{"DSO_VERSION": "1.0/../../x"}, "not a valid release tag"},
		{"verification tool missing", func(f *fixture) { f.publish(f.key) }, map[string]string{"DSO_COSIGN_BIN": "/nonexistent/cosign"}, "cosign is required"},
		{"network failure", func(f *fixture) { f.publish(f.key) }, map[string]string{"DSO_RELEASE_BASE": "http://127.0.0.1:1"}, "failed to download"},
		{"signed archive with an unsafe path", func(f *fixture) {
			f.tarBytes = buildTarball(t, "#!/bin/sh\necho x\n", [2]string{"../escape", "evil"})
			f.publish(f.key)
		}, nil, "unsafe paths"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.setup(f)
			r := f.install(c.env, "")
			f.assertNothingInstalled(r)
			if !strings.Contains(r.out, c.want) {
				t.Fatalf("expected %q in output:\n%s", c.want, r.out)
			}
			if _, err := os.Lstat(filepath.Join(f.home, "..", "escape")); err == nil {
				t.Fatal("path traversal wrote outside the temp directory")
			}
		})
	}
}

func TestInstaller_UnsupportedPlatformAndArchitecture(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	for name, uname := range map[string]string{
		"windows":      "#!/bin/sh\ncase \"$1\" in -s) echo Windows_NT;; -m) echo x86_64;; esac\n",
		"riscv64 arch": "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo riscv64;; esac\n",
	} {
		r := f.install(nil, uname)
		f.assertNothingInstalled(r)
		if !strings.Contains(r.out, "Unsupported") {
			t.Fatalf("%s: %s", name, r.out)
		}
	}
	// A release published only for the OTHER architecture: the exact file name is required.
	other := "amd64"
	if runtime.GOARCH == "amd64" {
		other = "arm64"
	}
	g := newFixture(t)
	g.tarName = strings.Replace(g.tarName, runtime.GOARCH, other, 1)
	g.publish(g.key)
	r := g.install(nil, "")
	g.assertNothingInstalled(r)
}

func TestInstaller_FailedInstallationPreservesTheExistingInstallation(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	dir := filepath.Dir(f.installed())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.installed(), []byte("OLD-WORKING-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Verification passes but the destination cannot be written: the old binary must survive.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	r := f.install(nil, "")
	if r.code == 0 {
		t.Fatalf("install into a read-only directory must fail:\n%s", r.out)
	}
	b, _ := os.ReadFile(f.installed())
	if string(b) != "OLD-WORKING-BINARY" {
		t.Fatalf("the existing installation was damaged: %q", b)
	}
	// A failed VERIFICATION also leaves it untouched.
	_ = os.Chmod(dir, 0o755)
	f.write(f.tarName, buildTarball(t, "#!/bin/sh\necho EVIL\n"))
	if r := f.install(nil, ""); r.code == 0 {
		t.Fatal("must fail")
	}
	b, _ = os.ReadFile(f.installed())
	if string(b) != "OLD-WORKING-BINARY" {
		t.Fatalf("the existing installation was replaced by an unverified artifact: %q", b)
	}
}

func TestInstaller_UpgradeReplacesAnExistingInstallationAfterVerification(t *testing.T) {
	f := newFixture(t)
	f.publish(f.key)
	_ = os.MkdirAll(filepath.Dir(f.installed()), 0o755)
	_ = os.WriteFile(f.installed(), []byte("OLD"), 0o755)
	if r := f.install(nil, ""); r.code != 0 {
		t.Fatalf("%s", r.out)
	}
	if b, _ := os.ReadFile(f.installed()); !strings.Contains(string(b), "new-binary") {
		t.Fatalf("%q", b)
	}
}

// Keyless mode (the production path) needs a real GitHub OIDC signature, which cannot
// be produced here. This verifies, through a recording cosign shim, that the installer
// asks for EXACTLY the release workflow identity for the requested tag and the GitHub
// Actions issuer, passes the certificate and detached signature, and fails closed on
// any verifier failure or missing signature/certificate.
func TestInstaller_KeylessModeRequestsTheExactWorkflowIdentityAndFailsClosed(t *testing.T) {
	goos, goarch := platform(t)
	build := func(t *testing.T, shimExit int, skip ...string) (home string, argsFile string, r result) {
		t.Helper()
		root := t.TempDir()
		rel := filepath.Join(root, "v"+version)
		_ = os.MkdirAll(rel, 0o755)
		tb := buildTarball(t, "#!/bin/sh\necho keyless\n")
		name := fmt.Sprintf("dso-%s-%s-%s.tar.gz", version, goos, goarch)
		_ = os.WriteFile(filepath.Join(rel, name), tb, 0o644)
		manifest := fmt.Sprintf("%s  %s\n", sha(tb), name)
		cs := fmt.Sprintf("dso-%s-checksums.txt", version)
		_ = os.WriteFile(filepath.Join(rel, cs), []byte(manifest), 0o644)
		_ = os.WriteFile(filepath.Join(rel, cs+".sig"), []byte("sig"), 0o644)
		_ = os.WriteFile(filepath.Join(rel, cs+".pem"), []byte("pem"), 0o644)
		for _, s := range skip {
			_ = os.Remove(filepath.Join(rel, cs+s))
		}
		shimDir := t.TempDir()
		argsFile = filepath.Join(shimDir, "args")
		shim := fmt.Sprintf("#!/bin/sh\nif [ \"$2\" = \"--help\" ]; then echo '--new-bundle-format'; exit 0; fi\nprintf '%%s\\n' \"$@\" > %q\nexit %d\n", argsFile, shimExit)
		shimBin := filepath.Join(shimDir, "cosign")
		_ = os.WriteFile(shimBin, []byte(shim), 0o755)
		home = t.TempDir()
		cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts/install.sh"))
		cmd.Env = []string{"HOME=" + home, "PATH=" + stubBin(t, "") + ":" + os.Getenv("PATH"), "DSO_VERSION=" + version, "DSO_RELEASE_BASE=file://" + root, "DSO_COSIGN_BIN=" + shimBin}
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return home, argsFile, result{string(out), code}
	}

	home, argsFile, r := build(t, 0)
	if r.code != 0 {
		t.Fatalf("keyless install with an accepting verifier must succeed: %s", r.out)
	}
	args, _ := os.ReadFile(argsFile)
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	joined := " " + strings.Join(got, " ") + " "
	for _, must := range []string{
		" verify-blob ",
		" --certificate-identity https://github.com/docker-secret-operator/dso/.github/workflows/release.yml@refs/tags/v" + version + " ",
		" --certificate-oidc-issuer https://token.actions.githubusercontent.com ",
		" --new-bundle-format=false ",
	} {
		if !strings.Contains(strings.Join(got, " ")+" ", strings.TrimPrefix(must, " ")) && !strings.Contains(joined, must) {
			t.Fatalf("verifier arguments lack %q: %v", must, got)
		}
	}
	if strings.Contains(joined, "--insecure-ignore-tlog") || strings.Contains(joined, "--certificate-identity-regexp") {
		t.Fatalf("keyless verification must not weaken identity or transparency checks: %v", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".docker/cli-plugins/docker-dso")); err != nil {
		t.Fatal("binary not installed after a successful verification")
	}

	// verifier rejects -> nothing installed
	home, _, r = build(t, 1)
	if r.code == 0 || !strings.Contains(r.out, "signature verification FAILED") {
		t.Fatalf("a rejecting verifier must abort: %s", r.out)
	}
	if _, err := os.Stat(filepath.Join(home, ".docker/cli-plugins/docker-dso")); err == nil {
		t.Fatal("installed despite a failed verification")
	}
	// missing signature / certificate -> refuse
	for _, skip := range []string{".sig", ".pem"} {
		home, _, r = build(t, 0, skip)
		if r.code == 0 {
			t.Fatalf("a release without %s must be refused: %s", skip, r.out)
		}
		if _, err := os.Stat(filepath.Join(home, ".docker/cli-plugins/docker-dso")); err == nil {
			t.Fatalf("installed without %s", skip)
		}
	}
}
