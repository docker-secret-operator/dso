package cli

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

func injectInspect(running bool, tmpfs map[string]string, mounts ...container.MountPoint) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:         strings.Repeat("a", 64),
			State:      &container.State{Running: running, Status: map[bool]string{true: "running", false: "exited"}[running]},
			HostConfig: &container.HostConfig{Tmpfs: tmpfs},
		},
		Mounts: mounts,
	}
}

func TestCheckInjectTarget(t *testing.T) {
	const dir = "/run/secrets/dso"
	cases := []struct {
		name    string
		in      container.InspectResponse
		dir     string
		wantErr string
	}{
		{"host-config tmpfs exact", injectInspect(true, map[string]string{dir: "mode=0700"}), dir, ""},
		{"host-config tmpfs parent", injectInspect(true, map[string]string{"/run": ""}), dir, ""},
		{"tmpfs trailing slash key", injectInspect(true, map[string]string{dir + "/": ""}), dir, ""},
		{"mounts tmpfs exact", injectInspect(true, nil, container.MountPoint{Type: mount.TypeTmpfs, Destination: dir}), dir, ""},
		{"no declared mount (writable layer)", injectInspect(true, nil), dir, "writable layer"},
		{"unrelated tmpfs", injectInspect(true, map[string]string{"/tmp": ""}), dir, "not inside a tmpfs"},
		{"sibling prefix is not a parent", injectInspect(true, map[string]string{"/run/secrets/dso2": ""}), dir, "not inside a tmpfs"},
		{"bind mount", injectInspect(true, nil, container.MountPoint{Type: mount.TypeBind, Destination: dir}), dir, "non-tmpfs mount"},
		{"named volume", injectInspect(true, nil, container.MountPoint{Type: mount.TypeVolume, Destination: dir}), dir, "non-tmpfs mount"},
		{"bind nested under tmpfs wins", injectInspect(true, map[string]string{"/run": ""}, container.MountPoint{Type: mount.TypeBind, Destination: "/run/secrets"}), dir, "non-tmpfs mount"},
		{"tmpfs nested under volume wins", injectInspect(true, map[string]string{dir: ""}, container.MountPoint{Type: mount.TypeVolume, Destination: "/run"}), dir, ""},
		{"stopped container", injectInspect(false, map[string]string{dir: ""}), dir, "not running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkInjectTarget(c.in, c.dir)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want refusal containing %q, got %v", c.wantErr, err)
			}
		})
	}
	paused := injectInspect(true, map[string]string{dir: ""})
	paused.State.Paused = true
	if err := checkInjectTarget(paused, dir); err == nil {
		t.Fatal("paused container must be refused")
	}
	if err := checkInjectTarget(container.InspectResponse{}, dir); err == nil {
		t.Fatal("missing state must be refused")
	}
}

func TestValidateInjectDestination(t *testing.T) {
	good := [][2]string{{"/run/secrets/dso", "api_key"}, {"/data", "a.b-c_1"}}
	for _, g := range good {
		if err := validateInjectDestination(g[0], g[1]); err != nil {
			t.Errorf("%v: %v", g, err)
		}
	}
	bad := [][2]string{
		{"run/secrets", "k"}, {"/run/secrets/", "k"}, {"/run/../etc", "k"}, {"//run/secrets", "k"},
		{"/", "k"}, {"", "k"}, {"/run/secrets\x00", "k"},
		{"/run/secrets", "../x"}, {"/run/secrets", "a/b"}, {"/run/secrets", ".."}, {"/run/secrets", "."},
		{"/run/secrets", ""}, {"/run/secrets", "a b"}, {"/run/secrets", "a\\b"}, {"/run/secrets", "a;b"},
	}
	for _, b := range bad {
		if err := validateInjectDestination(b[0], b[1]); err == nil {
			t.Errorf("%q %q should be rejected", b[0], b[1])
		}
	}
}

func TestInject_DefaultMountIsTmpfsDirectory(t *testing.T) {
	if got := NewInjectCmd().Flag("mount").DefValue; got != "/run/secrets/dso" {
		t.Fatalf("default --mount = %q", got)
	}
}

func TestInject_RejectsUnsafeDestinationBeforeAnyDockerCall(t *testing.T) {
	err, _, _ := runInject(t, "", "--container", "x", "--secret", "a/b", "--value", "v")
	if err == nil || !strings.Contains(err.Error(), "plain file name") {
		t.Fatalf("got %v", err)
	}
	err, _, _ = runInject(t, "", "--container", "x", "--secret", "k", "--mount", "/run/../etc", "--value", "v")
	if err == nil || !strings.Contains(err.Error(), "invalid --mount") {
		t.Fatalf("got %v", err)
	}
}
