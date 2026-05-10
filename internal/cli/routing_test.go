package cli

import (
	"io"
	"strings"
	"testing"
)

func TestRoutingCoverage(t *testing.T) {
	// Root commands
	cmds := []string{
		"up --help",
		"down --help",
		"logs --help",
		"inspect --help",
		"fetch --help",
		"metadata --help",
		"validate --help",
		"watch --help",
		"system --help",
		"secret --help",
		"env --help",
		"apply --help",
		"inject --help",
		"sync --help",
		"version",
	}

	for _, c := range cmds {
		args := strings.Split(c, " ")
		cmd := NewRootCmd()
		cmd.SetArgs(args)
		// We capture stdout to avoid clutter
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		_ = cmd.Execute()
	}
}
