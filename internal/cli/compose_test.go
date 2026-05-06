package cli

import (
	"testing"
)

func TestExtractConfigFromArgs(t *testing.T) {
	cfg := extractConfigFromArgs([]string{"--config", "my.yaml"})
	if cfg != "my.yaml" {
		t.Fatal("expected my.yaml")
	}

	cfg = extractConfigFromArgs([]string{"--config=my2.yaml"})
	if cfg != "my2.yaml" {
		t.Fatal("expected my2.yaml")
	}

	cfg = extractConfigFromArgs([]string{"-c", "my3.yaml"})
	if cfg != "my3.yaml" {
		t.Fatal("expected my3.yaml")
	}
}

func TestSplitEnv(t *testing.T) {
	k, v := splitEnv("A=B")
	if k != "A" || v != "B" {
		t.Fatal("expected A, B")
	}

	k, v = splitEnv("A")
	if k != "A" || v != "" {
		t.Fatal("expected A, ''")
	}
}

func TestValidateDockerArgs(t *testing.T) {
	err := validateDockerArgs([]string{"up", "-d"})
	if err != nil {
		t.Fatal(err)
	}
}
