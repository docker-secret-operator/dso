package installer

// [M0-05] Static consistency tests between the release workflow, GoReleaser config,
// installers and docs: the signing identity the workflow produces must be exactly the
// identity install.sh demands, and the release inputs must be pinned.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestContract_SigningIdentityMatchesInstaller(t *testing.T) {
	inst := readRepo(t, "scripts/install.sh")
	wf := readRepo(t, ".github/workflows/release.yml")
	gr := readRepo(t, ".goreleaser.yml")

	repo := regexp.MustCompile(`(?m)^REPO="([^"]+)"`).FindStringSubmatch(inst)
	if repo == nil {
		t.Fatal("REPO constant not found in install.sh")
	}
	identity := regexp.MustCompile(`SIGNER_IDENTITY="([^"]+)"`).FindStringSubmatch(inst)
	issuer := regexp.MustCompile(`SIGNER_ISSUER="([^"]+)"`).FindStringSubmatch(inst)
	if identity == nil || issuer == nil {
		t.Fatal("signer identity/issuer not found in install.sh")
	}
	// The workflow file the identity names must exist at that path and be the one that signs.
	const wantIdentity = "https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${TAG}"
	if identity[1] != wantIdentity {
		t.Fatalf("install.sh identity %q != %q", identity[1], wantIdentity)
	}
	if issuer[1] != "https://token.actions.githubusercontent.com" {
		t.Fatalf("issuer %q", issuer[1])
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), ".github/workflows/release.yml")); err != nil {
		t.Fatal(err)
	}
	// Sigstore's certificate SAN for a GitHub workflow is <repo>/<workflow path>@<ref>;
	// the workflow triggers on tag pushes, so the ref is refs/tags/<tag>.
	if !regexp.MustCompile(`(?s)on:\s*push:\s*tags:\s*-\s*"v\*"`).MatchString(wf) {
		t.Error("release.yml must be triggered by v* tag pushes so the signing ref is refs/tags/<tag>")
	}
	if strings.Contains(wf, "workflow_dispatch") || strings.Contains(wf, "workflow_call") || strings.Contains(wf, "pull_request") {
		t.Error("release.yml gained a trigger that would sign with a non-tag ref identity")
	}
	if !strings.Contains(wf, "id-token: write") {
		t.Error("release.yml must request id-token: write for keyless signing")
	}
	// Installer must not accept regexp identities / skip transparency in keyless mode.
	if strings.Contains(inst, "certificate-identity-regexp") {
		t.Error("install.sh must use an exact --certificate-identity")
	}
	// Repository named in the installer must be this repository's origin.
	if !strings.Contains(readRepo(t, ".git/config"), "docker-secret-operator/dso") {
		t.Log("origin remote differs from install.sh REPO; check the fork/release repo")
	}
	// GoReleaser signs the CHECKSUM file with detached files, in the format install.sh verifies.
	for _, must := range []string{"artifacts: checksum", "--output-signature=", "--output-certificate=", "--new-bundle-format=false", "--use-signing-config=false"} {
		if !strings.Contains(gr, must) {
			t.Errorf(".goreleaser.yml signing config lacks %q", must)
		}
	}
	if !strings.Contains(gr, `name_template: "{{ .ProjectName }}-{{ .Version }}-checksums.txt"`) {
		t.Error("checksum file name template no longer matches dso-<version>-checksums.txt")
	}
	if !strings.Contains(inst, `CHECKSUM_NAME="dso-${VERSION}-checksums.txt"`) {
		t.Error("install.sh checksum name changed")
	}
}

func TestContract_ReleaseInputsArePinned(t *testing.T) {
	wf := readRepo(t, ".github/workflows/release.yml")
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	for _, m := range regexp.MustCompile(`(?m)^\s*(?:-\s*)?uses:\s*(\S+)`).FindAllStringSubmatch(wf, -1) {
		parts := strings.SplitN(m[1], "@", 2)
		if len(parts) != 2 || !sha.MatchString(parts[1]) {
			t.Errorf("action %q is not pinned by full commit SHA", m[1])
		}
	}
	if !strings.Contains(wf, "sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532") || !regexp.MustCompile(`cosign-release:\s*v3\.1\.4\b`).MatchString(wf) {
		t.Error("cosign-installer and cosign-release must both be pinned")
	}
	if !regexp.MustCompile(`version:\s*v2\.\d+\.\d+\b`).MatchString(wf) {
		t.Error("goreleaser version must be an exact release")
	}
	ci := readRepo(t, ".github/workflows/ci.yml")
	if strings.Contains(ci, "@latest") {
		t.Error("ci.yml has an @latest tool install")
	}
	for _, need := range []string{"GOFLAGS", "check-release-env.sh", "check-no-fault-hook.sh"} {
		if !strings.Contains(wf, need) {
			t.Errorf("release.yml lacks the %s guard", need)
		}
	}
	if !strings.Contains(readRepo(t, ".goreleaser.yml"), "check-no-fault-hook.sh") {
		t.Error(".goreleaser.yml build hooks must inspect every built binary")
	}
	df := readRepo(t, "Dockerfile")
	if !strings.Contains(df, `ENV GOFLAGS=""`) || !strings.Contains(df, "DSO_FAULT_") {
		t.Error("Dockerfile lacks the GOFLAGS reset and the DSO_FAULT_ binary check")
	}
	br := readRepo(t, "scripts/build-release.sh")
	if !strings.Contains(br, "check-release-env.sh") || !strings.Contains(br, "check-no-fault-hook.sh") {
		t.Error("build-release.sh lacks the guards")
	}
}

// Windows is not a release target: install.ps1 must refuse and must not build or
// install unverified code.
func TestContract_WindowsInstallerFailsClosed(t *testing.T) {
	gr := readRepo(t, ".goreleaser.yml")
	if strings.Contains(gr, "windows") {
		t.Skip("a Windows release target now exists: install.ps1 must verify it instead of refusing")
	}
	ps := readRepo(t, "scripts/install.ps1")
	var code []string
	for _, l := range strings.Split(ps, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			code = append(code, l)
		}
	}
	body := strings.Join(code, "\n")
	for _, bad := range []string{"git clone", "go build", "Copy-Item", "Invoke-WebRequest", "Invoke-RestMethod", "Remove-Item"} {
		if strings.Contains(body, bad) {
			t.Errorf("install.ps1 must not contain %q", bad)
		}
	}
	if !strings.Contains(body, "exit 1") {
		t.Error("install.ps1 must exit non-zero")
	}
}
