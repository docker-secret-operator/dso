package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/spf13/cobra"
)

var CfgFile string

// noColor disables ANSI colour in human-readable output. Set by --no-color or
// a non-empty NO_COLOR environment variable (https://no-color.org).
var noColor bool

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func colorOff() bool { return noColor || os.Getenv("NO_COLOR") != "" }

func stripIfNoColor(s string) string {
	if colorOff() {
		return ansiSeq.ReplaceAllString(s, "")
	}
	return s
}

func cprintf(format string, a ...any) { fmt.Print(stripIfNoColor(fmt.Sprintf(format, a...))) }
func cprintln(a ...any)               { fmt.Println(stripIfNoColor(fmt.Sprint(a...))) }
func cprint(a ...any)                 { fmt.Print(stripIfNoColor(fmt.Sprint(a...))) }

// cfgFileExplicit records whether -c/--config was actually passed on the
// command line, set from cmd.Flags().Changed("config") in NewRootCmd's
// PersistentPreRun. CfgFile alone can't tell us this: its flag default is
// literally "dso.yaml" (see NewRootCmd), so an operator who explicitly runs
// `-c dso.yaml` -- the single most obvious filename, used in every doc
// example -- ends up with the exact same CfgFile value as someone who never
// passed -c at all.
var cfgFileExplicit bool

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// systemConfigPath is the system-wide configuration file. A variable only so
// tests can point it at a temp file; production code never reassigns it.
var systemConfigPath = "/etc/dso/dso.yaml"

// Config source kinds reported by ResolveConfigSource.
const (
	ConfigFromFlag   = "flag"    // -c/--config
	ConfigFromSystem = "system"  // /etc/dso/dso.yaml
	ConfigFromLocal  = "local"   // ./dso.yaml
	ConfigFromUser   = "user"    // ~/.dso/dso.yaml
	ConfigFromNone   = "default" // nothing found; "dso.yaml" is returned and will not exist
)

// ConfigSource describes which configuration file was selected and why.
// It never carries file contents.
type ConfigSource struct {
	Path string
	Kind string
	// Shadowed lists other existing candidate files that were NOT selected
	// because a higher-priority source won.
	Shadowed []string
}

func userConfigPath() string { return config.UserConfigPath() }

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ResolveConfigSource selects the configuration file. Precedence, highest first:
//
//  1. -c/--config, whenever it was actually passed (even if it is "dso.yaml")
//  2. /etc/dso/dso.yaml        (system-wide; kept ahead of ./dso.yaml for
//     compatibility with existing installations)
//  3. ./dso.yaml               (current directory)
//  4. ~/.dso/dso.yaml          (per-user; used only when none of the above exist)
//
// If nothing exists, "dso.yaml" is returned so callers report a missing file.
func ResolveConfigSource() ConfigSource {
	candidates := []struct{ kind, path string }{
		{ConfigFromSystem, systemConfigPath},
		{ConfigFromLocal, "dso.yaml"},
		{ConfigFromUser, userConfigPath()},
	}

	var src ConfigSource
	if CfgFile != "" && (CfgFile != "dso.yaml" || cfgFileExplicit) {
		src = ConfigSource{Path: CfgFile, Kind: ConfigFromFlag}
	} else {
		src = ConfigSource{Path: "dso.yaml", Kind: ConfigFromNone}
		for _, c := range candidates {
			if fileExists(c.path) {
				src = ConfigSource{Path: c.path, Kind: c.kind}
				break
			}
		}
	}
	for _, c := range candidates {
		if c.path != "" && c.path != src.Path && fileExists(c.path) {
			src.Shadowed = append(src.Shadowed, c.path)
		}
	}
	return src
}

// ResolveConfig returns the selected configuration path.
func ResolveConfig() string {
	return ResolveConfigSource().Path
}

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dso",
		Short: "Docker Secret Operator (DSO) — Secret lifecycle runtime for Docker Compose",
		Long: `Docker Secret Operator (DSO) is a cloud-native secret injection runtime for Docker Compose.
It fetches and injects secrets into containers at runtime without exposing them to the host filesystem.

Quick start:
  docker dso setup                  # Interactive setup wizard (recommended)

Usage:
  docker dso setup                  # Interactive setup wizard (handles everything)

Advanced Usage (Manual Bootstrapping):
  docker dso bootstrap local        # Lower-level manual local mode init
  sudo docker dso bootstrap agent   # Lower-level manual production init

Quick reference:
  docker dso doctor                 # Validate environment
  docker dso status                 # Check operational status
  docker dso config show            # View configuration

DSO supports multiple secret backends: local vault, HashiCorp Vault, AWS Secrets Manager, and Azure Key Vault.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cfgFileExplicit = cmd.Flags().Changed("config")
			_, _ = observability.NewLogger("info", "console", false)
		},
	}

	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable ANSI colour in output (also honours NO_COLOR)")
	cmd.PersistentFlags().StringVarP(&CfgFile, "config", "c", "dso.yaml", "config file (default search order: /etc/dso/dso.yaml, ./dso.yaml, ~/.dso/dso.yaml; see 'dso config path')")

	cmd.AddCommand(NewSetupCmd())
	cmd.AddCommand(NewBootstrapCmd())
	cmd.AddCommand(NewDoctorCmd())
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewConfigCmd())
	cmd.AddCommand(NewSystemCmd())
	cmd.AddCommand(NewAgentCmd())
	cmd.AddCommand(NewMetadataCmd())
	cmd.AddCommand(NewComposeCmd())
	cmd.AddCommand(NewFetchCmd())
	cmd.AddCommand(NewInitCmd())
	cmd.AddCommand(NewApplyCmd())
	cmd.AddCommand(NewInjectCmd())
	cmd.AddCommand(NewSyncCmd())
	cmd.AddCommand(NewMigrateCmd())
	cmd.AddCommand(NewNotificationsCmd())
	cmd.AddCommand(NewUpCmd())
	cmd.AddCommand(NewDownCmd())
	cmd.AddCommand(NewWatchCmd())
	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewExportCmd())
	cmd.AddCommand(NewInspectCmd())
	cmd.AddCommand(NewDiffCmd())
	cmd.AddCommand(NewLogsCmd())
	cmd.AddCommand(NewSecretCmd())
	cmd.AddCommand(NewEnvImportCmd())
	cmd.AddCommand(NewCentralCmd())

	// Errors are reported exactly once, by Run, on stderr. Cobra's own
	// printing is silenced so runtime failures don't also dump usage text.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	enforceGroupCommandArgs(cmd)

	return cmd
}

// enforceGroupCommandArgs makes every command that has subcommands reject
// stray positional arguments (an unknown subcommand or typo) with an error
// instead of silently printing help and exiting 0. A bare group command with
// no arguments still prints its help, as before. Commands that declare their
// own Args validator are left alone.
func enforceGroupCommandArgs(c *cobra.Command) {
	if c.HasSubCommands() {
		if c.Args == nil {
			c.Args = cobra.NoArgs
		}
		if !c.Runnable() {
			c.RunE = func(cmd *cobra.Command, args []string) error { return cmd.Help() }
		}
	}
	for _, sub := range c.Commands() {
		enforceGroupCommandArgs(sub)
	}
}

// Execute runs the CLI against os.Args and exits the process with the
// resulting status. All logic lives in Run so it can be tested without
// terminating the test binary.
func Execute() {
	// Docker CLI plugin fix: strip the plugin name if it's passed as the first argument
	// (Required when called via 'docker dso ...')
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "dso" {
		args = args[1:]
	}
	os.Exit(Run(args, os.Stdout, os.Stderr))
}

// Run executes the root command with args and returns the process exit code:
// 0 on success, 1 on any error (or the specific status of an exitCodeError). The error is printed once to stderr.
func Run(args []string, stdout, stderr io.Writer) int {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs(args)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return exitCodeFor(err)
	}
	return 0
}
