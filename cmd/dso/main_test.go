package main

import "testing"

func TestMainPackage_Dummy(t *testing.T) {
	// This is a dummy test to satisfy the Go test runner for the cmd/dso package.
	// We also verify the version variables exist and hold the default values.
	if version != "dev" || commit != "none" || date != "unknown" {
		t.Errorf("Default version variables were not as expected")
	}
	t.Log("cmd/dso package loaded successfully")
}
