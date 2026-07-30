package cli

import (
	"testing"
)

func TestApplyCommandCoverage(t *testing.T) {
	cmd := NewApplyCmd()
	_ = cmd.Flags().Set("dry-run", "true")
	_ = applyCommand(cmd, []string{})
}

func TestInjectCommandCoverage(t *testing.T) {
	cmd := NewInjectCmd()
	_ = cmd.Flags().Set("container", "dummy")
	_ = cmd.Flags().Set("secret", "dummy")
	_ = injectCommand(cmd, []string{})
}

func TestSyncCommandCoverage(t *testing.T) {
	cmd := NewSyncCmd()
	_ = syncCommand(cmd, []string{})
}

func TestSystemSetupCmdCoverage(t *testing.T) {
	cmd := newSystemSetupCmd()
	_ = cmd.Flags().Set("dry-run", "true")
	_ = cmd.RunE(cmd, []string{})
}
