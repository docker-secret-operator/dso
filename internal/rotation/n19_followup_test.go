package rotation

// N19 follow-up checks (docs/design/n19-managed-delivery.md): what a gate release may
// be authorised by, and what the post-delivery probe proves. Docker-free.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func runningGateInspect(t *testing.T, id string, specs []FileSecretSpec) container.InspectResponse {
	t.Helper()
	cfg := &container.Config{Entrypoint: []string{"/app/entry"}, Cmd: []string{"serve"}}
	hc := &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": ""}}
	if err := PrepareGatedCandidate(cfg, hc, specs, 0); err != nil {
		t.Fatal(err)
	}
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: id, Path: "/bin/sh", State: &container.State{Running: true}, HostConfig: hc},
		Config:            cfg,
		Mounts:            []container.MountPoint{{Type: mount.TypeTmpfs, Destination: "/run/secrets/dso", RW: true}},
	}
}

// A gate armed for one file set must not be released by a delivery of another: the files
// the gate waits for must all be written (and then probed) by this very delivery.
func TestVerifyRunningGate_GateFileSetMustBeCoveredByTheDelivery(t *testing.T) {
	armed := gateSpecs("/run/secrets/dso") // files a, b
	insp := runningGateInspect(t, "c1", armed)
	if err := VerifyRunningGate(insp, "c1", "", armed); err != nil {
		t.Fatalf("the matching delivery was refused: %v", err)
	}
	superset := []FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{"a": "x", "b": "y", "c": "z"}, UID: 1000, GID: 1000}}
	if err := VerifyRunningGate(insp, "c1", "", superset); err != nil {
		t.Fatalf("a delivery that also writes the gate's files is fine: %v", err)
	}
	for name, specs := range map[string][]FileSecretSpec{
		"renamed file":    {{Path: "/run/secrets/dso", Data: map[string]string{"a": "x", "other": "y"}, UID: 1000, GID: 1000}},
		"missing file":    {{Path: "/run/secrets/dso", Data: map[string]string{"a": "x"}, UID: 1000, GID: 1000}},
		"other directory": {{Path: "/run/secrets/dso/sub", Data: map[string]string{"a": "x", "b": "y"}, UID: 1000, GID: 1000}},
	} {
		err := VerifyRunningGate(insp, "c1", "", specs)
		if err == nil || !errors.Is(err, ErrDeliveryRefused) {
			t.Fatalf("%s: a delivery for a different file set was accepted: %v", name, err)
		}
	}
}

// A stale or foreign marker authorises nothing: the identity, the rotation id of the
// operation, and the gate's own flag path are all checked before a byte is written.
func TestVerifyRunningGate_ExactIdentityAndFlagPath(t *testing.T) {
	specs := gateSpecs("/run/secrets/dso")
	insp := runningGateInspect(t, "abcdef0123456789abcdef0123456789", specs)
	if err := VerifyRunningGate(insp, "abcdef0123456789abcdef0123456789", "", specs); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRunningGate(insp, "ffffffffffffffffffffffffffffffff", "", specs); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("a different container id was accepted: %v", err)
	}
	if err := VerifyRunningGate(insp, "abcdef0123456789abcdef0123456789", "rot-other", specs); err == nil {
		t.Fatal("an operation whose rotation id the container does not carry was accepted")
	}
	// the flag path the gate waits for must be the one this delivery writes
	other := []FileSecretSpec{{Path: "/run/secrets/elsewhere", Data: map[string]string{"a": "x", "b": "y"}, UID: 1000, GID: 1000}}
	if err := VerifyRunningGate(insp, "abcdef0123456789abcdef0123456789", "", other); err == nil {
		t.Fatal("a delivery whose release flag differs from the gate's was accepted")
	}
}

// Every destination file is verified: a probe failure of ANY spec stops the release, and
// the release flag name can never be a user secret file name.
func TestDelivery_EveryFileProbedBeforeRelease_FlagNameReserved(t *testing.T) {
	collide := []FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{GateFlagName: "v"}, UID: 1, GID: 1}}
	if err := ValidateFileSecretSpecs(collide); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("a secret file named like the release marker was accepted: %v", err)
	}

	two := []FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"a": "x"}, UID: 1000, GID: 1000},
		{Path: "/run/secrets/dso/more", Data: map[string]string{"b": "y"}, UID: 1000, GID: 1000},
	}
	cfg := &container.Config{Entrypoint: []string{"/app/entry"}}
	hc := &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": ""}}
	if err := PrepareGatedCandidate(cfg, hc, two, 0); err != nil {
		t.Fatal(err)
	}
	insp := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: "c1", Path: "/bin/sh", State: &container.State{Running: true}, HostConfig: hc},
		Config:            cfg,
		Mounts:            []container.MountPoint{{Type: mount.TypeTmpfs, Destination: "/run/secrets/dso", RW: true}},
	}
	var probed []string
	released := false
	ops := DeliveryOps{
		Inspect: func(context.Context, string) (container.InspectResponse, error) { return insp, nil },
		Inject:  func(context.Context, string, FileSecretSpec) error { return nil },
		Probe: func(_ context.Context, _ string, s FileSecretSpec) error {
			probed = append(probed, s.Path)
			if s.Path == "/run/secrets/dso/more" {
				return errors.New("second destination missing")
			}
			return nil
		},
		Release: func(context.Context, string, string, int, int) error { released = true; return nil },
	}
	_, err := (&Delivery{Ops: ops}).Execute(context.Background(), "c1", two)
	if err == nil || released {
		t.Fatalf("err=%v released=%v: the gate was released although a destination was not verified", err, released)
	}
	if len(probed) != 2 {
		t.Fatalf("probed %v: every destination must be probed", probed)
	}
}

// The probe also requires the file to be readable by the user it runs as (the container
// user): a root-only file for a non-root application user would otherwise pass.
func TestProbeScript_UnreadableFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	d := t.TempDir()
	p := filepath.Join(d, "locked")
	if err := os.WriteFile(p, []byte("v"), 0o200); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/bin/sh", "-c", probeScript, "dso-probe", p).Run(); err == nil {
		t.Fatal("a file the probing user cannot read passed the probe")
	}
	if err := os.WriteFile(filepath.Join(d, "locked2"), []byte("v"), 0o200); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/bin/sh", "-c", probeScript, "dso-probe", d).Run(); err == nil {
		t.Fatal("a directory holding an unreadable file passed the probe")
	}
}
