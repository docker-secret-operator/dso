package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectMode(t *testing.T) {
	mode, reason := detectMode("local", "")
	if mode != "local" || reason != "flag" {
		t.Fatal("expected local/flag")
	}

	os.Setenv("DSO_MODE", "cloud")
	mode, reason = detectMode("", "")
	if mode != "cloud" || reason != "env" {
		t.Fatal("expected cloud/env")
	}
	os.Unsetenv("DSO_MODE")

	mode, _ = detectMode("", "missing.yaml")
	if mode != "cloud" {
		t.Fatal("expected cloud")
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

	os.Setenv("COMPOSE_PROJECT_NAME", "myproj3")
	name = getProjectName([]string{})
	if name != "myproj3" {
		t.Fatal("expected myproj3")
	}
	os.Unsetenv("COMPOSE_PROJECT_NAME")

	name = getProjectName([]string{})
	dir, _ := os.Getwd()
	if name != filepath.Base(dir) {
		t.Fatal("expected cwd base")
	}
}
