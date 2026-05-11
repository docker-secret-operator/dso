package cli

import (
	"testing"
)

func TestApplyCommandCoverage(t *testing.T) {
	cmd := NewApplyCmd()
	cmd.Flags().Set("dry-run", "true")
	_ = applyCommand(cmd, []string{})
}

func TestInjectCommandCoverage(t *testing.T) {
	cmd := NewInjectCmd()
	cmd.Flags().Set("container", "dummy")
	cmd.Flags().Set("secret", "dummy")
	_ = injectCommand(cmd, []string{})
}

func TestSyncCommandCoverage(t *testing.T) {
	cmd := NewSyncCmd()
	_ = syncCommand(cmd, []string{})
}

func TestSystemSetupCmdCoverage(t *testing.T) {
	cmd := newSystemSetupCmd()
	cmd.Flags().Set("dry-run", "true")
	_ = cmd.RunE(cmd, []string{})
}
