package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"github.com/spf13/cobra"
)

const migratedComposeFilename = "docker-compose.dso.yml"

// NewMigrateCmd creates `docker dso migrate`: the guided path from an
// existing .env + Compose project to a DSO-managed one.
func NewMigrateCmd() *cobra.Command {
	var (
		dryRun      bool
		confirm     bool
		overwrite   bool
		envFileFlag string
		composeFlag string
		projectFlag string
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate an existing .env + Compose project to DSO-managed secrets",
		Long: `Migrate an existing .env + Compose project to DSO-managed secrets.

Scans the current directory for a Compose file and a .env file, proposes
which environment variables look like secrets, and -- only after an
explicit preview and confirmation -- imports the selected values into the
DSO vault and writes a new docker-compose.dso.yml with dso:// references
in their place.

Your original docker-compose.yml and .env are never modified or deleted.

Examples:
  docker dso migrate              # interactive: preview, then ask to proceed
  docker dso migrate --dry-run    # preview only; never touches the vault or filesystem
  docker dso migrate --confirm    # apply without an interactive prompt (CI)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(migrateOptions{
				dryRun:      dryRun,
				confirm:     confirm,
				overwrite:   overwrite,
				envFileFlag: envFileFlag,
				composeFlag: composeFlag,
				projectFlag: projectFlag,
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the migration plan; never modifies the vault or filesystem")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Apply without an interactive prompt (for CI/non-interactive use)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing vault secrets that differ from the .env value (default: skip them)")
	cmd.Flags().StringVar(&envFileFlag, "env-file", ".env", "Path to the .env file to migrate")
	cmd.Flags().StringVar(&composeFlag, "file", "", "Path to the Compose file (default: auto-detected)")
	cmd.Flags().StringVar(&projectFlag, "project", "", "Vault project name (default: current directory name)")

	return cmd
}

type migrateOptions struct {
	dryRun      bool
	confirm     bool
	overwrite   bool
	envFileFlag string
	composeFlag string
	projectFlag string
}

func runMigrate(opts migrateOptions) error {
	composePath := opts.composeFlag
	if composePath == "" {
		found, ok := findComposeFile()
		if !ok {
			return errors.New("no docker-compose.yml/.yaml found in the current directory (use --file to specify one)")
		}
		composePath = found
	}

	envPath := opts.envFileFlag
	if _, err := os.Stat(envPath); err != nil {
		return fmt.Errorf("no .env file found at %s -- nothing to migrate (use --env-file to specify a different path)", envPath)
	}

	if _, err := config.IsSafePath("", envPath); err != nil {
		return fmt.Errorf("invalid --env-file path: %w", err)
	}
	if _, err := config.IsSafePath("", composePath); err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	project := opts.projectFlag
	if project == "" {
		project = getProjectName(nil)
	}

	lookup := buildVaultLookup(project)

	plan, err := planMigration(envPath, composePath, project, lookup)
	if err != nil {
		return fmt.Errorf("failed to build migration plan: %w", err)
	}

	printMigrationPreview(plan)

	if opts.dryRun {
		fmt.Println("\nNo files changed.")
		fmt.Println("No secrets imported.")
		return nil
	}

	if len(plan.Candidates) == 0 && len(plan.SelectedComposeChanges()) == 0 {
		fmt.Println("\nNothing selected to migrate.")
		return nil
	}

	if !opts.confirm {
		if !promptYesNo("\nProceed?") {
			fmt.Println("Migration cancelled. No changes made.")
			return nil
		}
	}

	v, err := loadOrInitVault()
	if err != nil {
		return fmt.Errorf("failed to access vault: %w", err)
	}

	summary := applySecrets(v, plan, opts.overwrite)
	printImportSummary(summary)

	successfulKeys := map[string]bool{}
	for _, k := range summary.Imported {
		successfulKeys[k] = true
	}
	for _, k := range summary.AlreadyExisted {
		successfulKeys[k] = true
	}
	var writableChanges []ComposeChange
	for _, c := range plan.SelectedComposeChanges() {
		if varName, ok := extractInterpolatedVar(c.OldValue); ok && successfulKeys[varName] {
			writableChanges = append(writableChanges, c)
		}
	}

	if len(writableChanges) > 0 {
		if err := writeMigratedCompose(composePath, migratedComposeFilename, writableChanges); err != nil {
			return fmt.Errorf("secrets were imported, but writing %s failed: %w", migratedComposeFilename, err)
		}
		fmt.Printf("\nWrote %s with %d secret reference(s).\n", migratedComposeFilename, len(writableChanges))
		fmt.Printf("Your original %s and %s were not modified.\n", composePath, envPath)
		fmt.Printf("Next: docker dso validate  (then review and adopt %s)\n", migratedComposeFilename)
	}

	if len(summary.Failed) > 0 {
		return fmt.Errorf("%d secret(s) failed to import -- see summary above", len(summary.Failed))
	}
	return nil
}

// buildVaultLookup returns a vaultLookupFunc backed by the real vault, or
// nil if the vault can't be opened for reading (e.g. not yet initialized).
// A nil lookup means planMigration simply skips conflict detection --
// there can be no conflicts against a vault that doesn't exist yet.
func buildVaultLookup(_ string) vaultLookupFunc {
	v, err := vault.LoadDefault()
	if err != nil {
		return nil
	}
	return func(project, key, candidateValue string) (exists bool, differs bool, err error) {
		sec, err := v.Get(project, key)
		if err != nil {
			return false, false, nil // not found is not an error here, just "doesn't exist"
		}
		return true, sec.Value != candidateValue, nil
	}
}

// loadOrInitVault loads the default vault, initializing it first if this
// is the first time DSO has been used on this machine. This keeps migrate
// usable as the very first DSO command a new user runs, without requiring
// a separate manual `docker dso init` step first.
func loadOrInitVault() (*vault.Vault, error) {
	v, err := vault.LoadDefault()
	if err == nil {
		return v, nil
	}
	if initErr := vault.InitDefault(); initErr != nil {
		return nil, err
	}
	fmt.Println("Initialized a new local DSO vault.")
	return vault.LoadDefault()
}

// printMigrationPreview renders the plan. Never prints a secret value --
// only key names, URIs, and counts.
func printMigrationPreview(plan *MigrationPlan) {
	fmt.Printf("Found:\n  %s\n  %s\n\n", plan.ComposePath, plan.EnvPath)
	fmt.Printf("Detected:\n  %d environment variable(s)\n\n", len(plan.AllVars))

	if len(plan.AlreadyMigrated) > 0 {
		fmt.Println("Already DSO-managed (skipped):")
		for _, k := range plan.AlreadyMigrated {
			fmt.Printf("  %s\n", k)
		}
		fmt.Println()
	}

	if len(plan.Candidates) > 0 {
		fmt.Println("Likely secrets:")
		for _, c := range plan.Candidates {
			mark := "[x]"
			if !c.Selected {
				mark = "[ ]"
			}
			suffix := ""
			if c.ExistsInVault && c.VaultDiffers {
				suffix = "  (conflict: vault has a different value)"
			} else if c.ExistsInVault {
				suffix = "  (already in vault, identical)"
			}
			fmt.Printf("  %s %s%s\n", mark, c.Key, suffix)
		}
		fmt.Println()
	}

	if len(plan.NonSecretVars) > 0 {
		fmt.Println("Other environment variables (not proposed for migration):")
		for _, k := range plan.NonSecretVars {
			fmt.Printf("  [ ] %s\n", k)
		}
		fmt.Println()
	}

	if len(plan.SelectedComposeChanges()) > 0 {
		fmt.Println("Proposed migration:")
		for _, c := range plan.SelectedComposeChanges() {
			fmt.Printf("  %-20s -> %s\n", c.EnvKey, c.NewURI)
		}
		fmt.Println()
	}

	if len(plan.ManualReview) > 0 {
		fmt.Println("Requires manual review:")
		for _, m := range plan.ManualReview {
			fmt.Printf("  %s: %s\n", m.Service, m.Reason)
		}
		fmt.Println()
	}

	if len(plan.DuplicateKeys) > 0 {
		fmt.Printf("Note: %d key(s) were duplicated in %s; the last value wins.\n\n", len(plan.DuplicateKeys), plan.EnvPath)
	}

	for _, w := range plan.Warnings {
		fmt.Printf("Warning: %s\n", w)
	}
}

func printImportSummary(s *ImportSummary) {
	fmt.Println()
	fmt.Printf("✓ Imported: %d\n", len(s.Imported))
	fmt.Printf("→ Already existed: %d\n", len(s.AlreadyExisted))
	fmt.Printf("⚠ Skipped: %d\n", len(s.Skipped))
	fmt.Printf("✗ Failed: %d\n", len(s.Failed))
	if len(s.Skipped) > 0 {
		fmt.Println("  (skipped keys already exist in the vault with a different value -- rerun with --overwrite to replace them)")
	}
	if len(s.Failed) > 0 {
		fmt.Println("  Failed keys:")
		for k, reason := range s.Failed {
			fmt.Printf("    %s: %s\n", k, reason)
		}
	}
}

func promptYesNo(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
