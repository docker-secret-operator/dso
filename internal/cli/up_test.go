package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectMode(t *testing.T) {
	mode, reason := detectMode("local", "")
	if mode != "local" || reason != "flag" {
		t.Fatal("expected local/flag")
	}

	_ = os.Setenv("DSO_MODE", "cloud")
	mode, reason = detectMode("", "")
	if mode != "cloud" || reason != "env" {
		t.Fatal("expected cloud/env")
	}
	_ = os.Unsetenv("DSO_MODE")

	mode, _ = detectMode("", "missing.yaml")
	if mode != "cloud" {
		t.Fatal("expected cloud")
	}
}

// TestSweepStaleTempComposeFiles verifies the LIFECYCLE-2 startup sweep only
// removes docker-compose-dso-*.yaml temp files older than staleTempComposeAge,
// so it cleans up files orphaned by a killed/crashed prior process without
// racing a concurrently-running `docker dso up`'s own fresh temp file.
func TestSweepStaleTempComposeFiles(t *testing.T) {
	staleFile, err := os.CreateTemp("", "docker-compose-dso-*.yaml")
	if err != nil {
		t.Fatalf("failed to create stale fixture: %v", err)
	}
	_ = staleFile.Close()
	defer func() { _ = os.Remove(staleFile.Name()) }()

	oldTime := time.Now().Add(-2 * staleTempComposeAge)
	if err := os.Chtimes(staleFile.Name(), oldTime, oldTime); err != nil {
		t.Fatalf("failed to backdate stale fixture: %v", err)
	}

	freshFile, err := os.CreateTemp("", "docker-compose-dso-*.yaml")
	if err != nil {
		t.Fatalf("failed to create fresh fixture: %v", err)
	}
	_ = freshFile.Close()
	defer func() { _ = os.Remove(freshFile.Name()) }()

	sweepStaleTempComposeFiles()

	if _, err := os.Stat(staleFile.Name()); !os.IsNotExist(err) {
		t.Errorf("expected stale temp compose file to be removed, stat err = %v", err)
	}
	if _, err := os.Stat(freshFile.Name()); err != nil {
		t.Errorf("expected fresh temp compose file to survive the sweep, stat err = %v", err)
	}
}

func TestGetProjectName(t *testing.T) {
	name := getProjectName([]string{"-p", "myproj"})
	if name != "myproj" {
		t.Fatal("expected myproj")
	}

	name = getProjectName([]string{"--project-name=myproj2"})
	if name != "myproj2" {
		t.Fatal("expected myproj2")
	}

	_ = os.Setenv("COMPOSE_PROJECT_NAME", "myproj3")
	name = getProjectName([]string{})
	if name != "myproj3" {
		t.Fatal("expected myproj3")
	}
	_ = os.Unsetenv("COMPOSE_PROJECT_NAME")

	name = getProjectName([]string{})
	dir, _ := os.Getwd()
	if name != filepath.Base(dir) {
		t.Fatal("expected cwd base")
	}
}
