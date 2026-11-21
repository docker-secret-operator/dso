package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/setup"
	"github.com/spf13/cobra"
)

// newPermissionsRepairer builds the repairer. A variable only so tests can
// point it at temporary directories; production code never reassigns it.
var newPermissionsRepairer = setup.NewPermissionsRepairer

// NewRepairCmd creates the `repair` command group.
func NewRepairCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair DSO host state that diagnostics reported as wrong",
		Long: `Repair groups explicit, narrowly scoped corrections for host state that
'dso doctor' can report. Nothing here runs automatically.`,
	}
	cmd.AddCommand(newRepairPermissionsCmd())
	return cmd
}

func newRepairPermissionsCmd() *cobra.Command {
	var dryRun, jsonOut bool
	cmd := &cobra.Command{
		Use:   "permissions",
		Short: "Inspect and repair the DSO directory permission contract",
		Long: `Inspect the DSO permission contract and correct directory-level deviations.

Checked paths (and only these):
  /etc/dso                          root:dso 0750
  /var/lib/dso                      root:dso 0750
  /var/run/dso (/run/dso)           root:dso 0750
  /var/lib/dso/ledger               root 0700
  /var/lib/dso/ledger/ledger.key    root 0400 (0600 accepted)
  /usr/local/lib/dso/plugins        root-owned, no group/world write

(root:root replaces root:dso when the host has no 'dso' group.)

What is repaired: the owner and mode of the directories above, one directory at
a time, through a descriptor opened with O_NOFOLLOW|O_DIRECTORY. A symlink or a
regular file at one of the paths is refused. A directory that is already
correct is not touched, so a second run changes nothing.

What is never done: no recursive chmod or chown; no configuration file (dso.yaml)
is read, rewritten or replaced; the ledger key is never created, regenerated,
renamed or removed (a missing, unsafe or corrupt key is reported with a
diagnostic, and its mode is only tightened when the file is verifiably a ledger
key owned by root); authentication state is not reset; the agent is not
restarted; no unrelated file is modified. Restart the agent yourself afterwards
if it was running with the old permissions.

Applying changes requires root. --dry-run needs no root and changes nothing.

Exit status:
  0   every contract path conforms, or all required corrections were applied
  1   not root (apply), a correction failed, or another error
  12  --dry-run: corrections are required (nothing was changed)
  13  a path needs manual action (symlink, wrong owner, missing or corrupt key)

Examples:
  docker dso repair permissions --dry-run   # show current vs expected, change nothing
  sudo docker dso repair permissions        # apply the directory-level corrections`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep, err := newPermissionsRepairer().Repair(dryRun)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				b, merr := json.MarshalIndent(rep, "", "  ")
				if merr != nil {
					return merr
				}
				_, _ = fmt.Fprintln(out, string(escapeJSONNonPrintable(b)))
			} else {
				rep.WriteText(out)
			}
			switch {
			case rep.Failed > 0:
				return fmt.Errorf("%d path(s) could not be repaired (%d changed); see the table above", rep.Failed, rep.Changed)
			case rep.DryRun && rep.ChangesRequired > 0:
				return &exitCodeError{exitRepairChangesRequired, fmt.Errorf("%d path(s) need correction (dry run: nothing was changed); run without --dry-run as root to apply", rep.ChangesRequired)}
			case rep.ManualRequired > 0:
				return &exitCodeError{exitRepairManualAction, errors.New("manual action is required for the path(s) marked 'manual'; this command does not change them")}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show current versus expected owner and mode; change nothing (no root needed)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}
