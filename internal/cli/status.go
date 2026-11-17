package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/spf13/cobra"
)

// NewStatusCmd creates the status operational visibility command
func NewStatusCmd() *cobra.Command {
	var (
		watchFlag bool
		jsonFlag  bool
		quietFlag bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show DSO runtime operational status",
		Long: `Display DSO runtime status including mode, providers, cache, and rotation health.

Provides operational visibility into the DSO system by querying the running
agent (via 'docker dso agent'/systemd) for its real, current state.

Examples:
  docker dso status              # Single status check
  docker dso status --watch      # Auto-refresh every 2 seconds
  docker dso status --json       # Machine-readable output

Use -q/--quiet to print nothing and rely on the exit status alone (errors
still go to stderr).

Exit status: 0 when the agent is reachable and healthy; 1 when the agent is
unreachable or reports degraded providers/services. Uptime is shown only when
it can be verified against a reachable agent; otherwise it is "unavailable".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			status := &Status{
				Watch: watchFlag,
				JSON:  jsonFlag,
				Out:   cmd.OutOrStdout(),
				Quiet: quietFlag,
			}
			return status.Run()
		},
	}

	cmd.Flags().BoolVar(&watchFlag, "watch", false, "Auto-refresh every 2 seconds")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
	cmd.Flags().BoolVarP(&quietFlag, "quiet", "q", false, "Print nothing; report health only through the exit status")

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// STATUS TYPES
// ════════════════════════════════════════════════════════════════════════════

type Status struct {
	Watch bool
	JSON  bool
	Quiet bool      // suppress output; exit status carries the result
	Out   io.Writer // defaults to os.Stdout
}

func (s *Status) out() io.Writer {
	if s.Out != nil {
		return s.Out
	}
	return os.Stdout
}

// Overall state values reported in SystemStatus.State.
const (
	StateHealthy     = "healthy"     // agent reachable, nothing unhealthy
	StateDegraded    = "degraded"    // agent reachable, but a provider or service is unhealthy
	StateUnavailable = "unavailable" // agent could not be queried
)

// RuntimeStatus describes the CLI build and local installation. Uptime and
// StartTime are omitted unless they can be verified: they require a reachable
// agent plus a well-formed start time on disk, never a guess.
type RuntimeStatus struct {
	Mode      string     `json:"mode"`
	Version   string     `json:"version"`
	Commit    string     `json:"commit,omitempty"`
	Uptime    string     `json:"uptime,omitempty"`
	StartTime *time.Time `json:"start_time,omitempty"`
}

// ProviderStatus reflects what the agent's SecretStoreManager actually
// knows about a provider — "unknown" when the agent hasn't contacted it
// yet, not a guess.
type ProviderStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // healthy, unhealthy, unknown
	Message string `json:"message,omitempty"`
}

// CacheStatus reports only what the agent actually tracks (entry count).
// Hit/miss/size instrumentation does not exist in the agent yet, so those
// fields are deliberately omitted rather than filled with invented numbers.
type CacheStatus struct {
	Entries int `json:"entries"`
}

// RotationStatus reports only what the agent's crash-recovery state tracker
// actually counts (in-flight/pending rotations). Historical success/failure
// totals are not tracked anywhere in the agent yet.
type RotationStatus struct {
	Pending int `json:"pending"`
}

// LedgerStatus reports the applied-version ledger's availability as the agent
// sees it: state is "active", "disabled", "not_configured" or "unknown" (an
// agent that predates the field). Disabled carries the cause code and the
// supported remediation; it never carries key material or secret values.
type LedgerStatus struct {
	State       string `json:"state"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Degraded    string `json:"degraded_behaviour,omitempty"`
}

type SystemStatus struct {
	Runtime      RuntimeStatus    `json:"runtime"`
	AgentReached bool             `json:"agent_reached"`
	AgentError   string           `json:"agent_error,omitempty"`
	Providers    []ProviderStatus `json:"providers,omitempty"`
	Cache        CacheStatus      `json:"cache"`
	Rotations    RotationStatus   `json:"rotations"`
	DegradedSvcs int              `json:"degraded_services"`
	Ledger       LedgerStatus     `json:"ledger"`
	State        string           `json:"state"`
	Health       string           `json:"health"`
}

// ════════════════════════════════════════════════════════════════════════════
// RUN METHOD
// ════════════════════════════════════════════════════════════════════════════

func (s *Status) Run() error {
	if s.Watch {
		return s.watchStatus()
	}

	return s.printStatus()
}

// printStatus renders one snapshot and returns a non-nil error when the system
// is not healthy so the process exits non-zero for scripts and health probes.
func (s *Status) printStatus() error {
	systemStatus := s.gatherStatus()

	var err error
	if s.Quiet {
		return systemStatus.exitError()
	}
	if s.JSON {
		err = s.printJSON(systemStatus)
	} else {
		err = s.printText(systemStatus)
	}
	if err != nil {
		return err
	}
	return systemStatus.exitError()
}

func (st SystemStatus) exitError() error {
	switch st.State {
	case StateHealthy:
		return nil
	case StateDegraded:
		return fmt.Errorf("DSO is degraded: one or more providers or services are unhealthy")
	default:
		return fmt.Errorf("DSO agent is unavailable: %s", st.AgentError)
	}
}

func (s *Status) watchStatus() error {
	for {
		// Clear screen (ANSI escape code)
		_, _ = fmt.Fprint(s.out(), "\033[2J\033[H")

		systemStatus := s.gatherStatus()
		if s.JSON {
			_ = s.printJSON(systemStatus)
		} else {
			_ = s.printText(systemStatus)
		}

		_, _ = fmt.Fprintln(s.out())
		_, _ = fmt.Fprintln(s.out(), "Refreshing in 2 seconds (Ctrl+C to exit)...")
		time.Sleep(2 * time.Second)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// STATUS GATHERING
// ════════════════════════════════════════════════════════════════════════════

func (s *Status) gatherStatus() SystemStatus {
	status := SystemStatus{}

	socketPath := "/run/dso/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	unavailable := func(err error) SystemStatus {
		status.Runtime = s.gatherRuntime(false)
		status.AgentReached = false
		status.AgentError = err.Error()
		status.State = StateUnavailable
		status.Health = "✗ agent unreachable"
		return status
	}

	client, err := injector.NewAgentClientWithTimeout(socketPath, 3*time.Second)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.GetStatus()
	if err != nil {
		return unavailable(err)
	}

	status.Runtime = s.gatherRuntime(true)
	status.AgentReached = true
	status.Cache = CacheStatus{Entries: resp.CacheEntries}
	status.Rotations = RotationStatus{Pending: resp.PendingRotations}
	status.DegradedSvcs = len(resp.DegradedServices)
	status.Ledger = LedgerStatus{State: resp.Ledger.State, Code: resp.Ledger.Code, Message: resp.Ledger.Message,
		Remediation: resp.Ledger.Remediation, Degraded: resp.Ledger.Degraded}
	if status.Ledger.State == "" {
		status.Ledger.State = "unknown" // an agent that predates the ledger status field
	}

	unhealthy := false
	for _, p := range resp.Providers {
		ps := ProviderStatus{Name: p.Name, Message: p.Message}
		switch {
		case !p.Known:
			ps.Status = "unknown"
		case p.Healthy:
			ps.Status = "healthy"
		default:
			ps.Status = "unhealthy"
			unhealthy = true
		}
		status.Providers = append(status.Providers, ps)
	}

	switch {
	case unhealthy || status.DegradedSvcs > 0:
		status.State = StateDegraded
		status.Health = "⚠ degraded: some providers or services unhealthy"
	default:
		status.State = StateHealthy
		status.Health = "✓ All systems nominal"
	}

	return status
}

// gatherRuntime reports facts that can actually be verified. The agent
// protocol does not expose its version or start time, so uptime is derived
// only when the agent is reachable now AND ~/.dso/state/runtime.json holds a
// well-formed start_time that is not in the future. A state file on its own
// proves nothing about a running agent, so it is ignored when agentReached is
// false.
func (s *Status) gatherRuntime(agentReached bool) RuntimeStatus {
	homeDir, _ := os.UserHomeDir()
	dsoDir := filepath.Join(homeDir, ".dso")

	mode := "unknown"
	if _, err := os.Stat("/etc/dso"); err == nil {
		mode = "agent"
	} else if _, err := os.Stat(dsoDir); err == nil {
		mode = "local"
	}

	version := Version
	if version == "" {
		version = "unknown"
	}
	commit := Commit
	if commit == "none" {
		commit = ""
	}

	rt := RuntimeStatus{Mode: mode, Version: version, Commit: commit}
	if agentReached {
		if st, ok := readVerifiedStartTime(filepath.Join(dsoDir, "state", "runtime.json"), time.Now()); ok {
			rt.StartTime = &st
			rt.Uptime = formatDuration(time.Since(st))
		}
	}
	return rt
}

// readVerifiedStartTime returns the start_time recorded in path, or false if
// the file is absent, malformed, or records a time in the future.
func readVerifiedStartTime(path string, now time.Time) (time.Time, bool) {
	// #nosec G304 -- path is built from the user's home dir and fixed literals
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	var state struct {
		StartTime string `json:"start_time"`
	}
	if err := json.Unmarshal(data, &state); err != nil || state.StartTime == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, state.StartTime)
	if err != nil || t.After(now) {
		return time.Time{}, false
	}
	return t, true
}

// ════════════════════════════════════════════════════════════════════════════
// OUTPUT METHODS
// ════════════════════════════════════════════════════════════════════════════

const statusInnerWidth = 61 // characters between the box's vertical borders

// boxRow renders one box line, padding/truncating by rune count so the right
// border always aligns regardless of multi-byte symbols.
func boxRow(text string) string {
	const textWidth = statusInnerWidth - 2 // one space of padding each side
	r := []rune(text)
	if len(r) > textWidth {
		r = append(r[:textWidth-1], '…')
	}
	return "│ " + string(r) + strings.Repeat(" ", textWidth-len(r)) + " │"
}

func (s *Status) printText(status SystemStatus) error {
	w := s.out()
	line := func(text string) { _, _ = fmt.Fprintln(w, boxRow(text)) }
	rule := strings.Repeat("─", statusInnerWidth)

	uptime := status.Runtime.Uptime
	if uptime == "" {
		uptime = "unavailable"
	}
	version := status.Runtime.Version
	if status.Runtime.Commit != "" {
		version += " (" + status.Runtime.Commit + ")"
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "┌"+rule+"┐")
	line("             DSO Runtime Status")
	_, _ = fmt.Fprintln(w, "├"+rule+"┤")
	line(fmt.Sprintf("Mode:     %s", status.Runtime.Mode))
	line(fmt.Sprintf("Version:  %s", version))
	line(fmt.Sprintf("Uptime:   %s", uptime))
	line("")

	if !status.AgentReached {
		line("AGENT: NOT REACHABLE")
		line("  " + status.AgentError)
		line("")
		line("HEALTH: " + status.Health)
		_, _ = fmt.Fprintln(w, "└"+rule+"┘")
		_, _ = fmt.Fprintln(w)
		return nil
	}

	line("PROVIDERS")
	if len(status.Providers) == 0 {
		line("└─ (none configured)")
	}
	for i, p := range status.Providers {
		prefix := "├─"
		if i == len(status.Providers)-1 {
			prefix = "└─"
		}
		line(fmt.Sprintf("%s %s:  %s %s", prefix, p.Name, statusSymbolInline(p.Status), p.Message))
	}
	line("")

	line("CACHE")
	line(fmt.Sprintf("└─ Entries:  %d", status.Cache.Entries))
	line("")

	line("ROTATIONS")
	line(fmt.Sprintf("└─ Pending:  %d", status.Rotations.Pending))
	line("")

	if status.AgentReached {
		line("LEDGER")
		line("└─ State:  " + status.Ledger.State + ledgerCodeSuffix(status.Ledger))
		if status.Ledger.State == "disabled" {
			for _, f := range [][2]string{{"Cause", status.Ledger.Message}, {"Degraded", status.Ledger.Degraded}, {"Fix", status.Ledger.Remediation}} {
				for i, l := range wrapWords(f[0]+": "+f[1], statusInnerWidth-8) {
					if i > 0 {
						l = "  " + l
					}
					line("   " + l)
				}
			}
		}
		line("")
	}

	if status.DegradedSvcs > 0 {
		line(fmt.Sprintf("DEGRADED SERVICES: %d", status.DegradedSvcs))
		line("")
	}

	line("HEALTH: " + status.Health)
	_, _ = fmt.Fprintln(w, "└"+rule+"┘")
	_, _ = fmt.Fprintln(w)

	return nil
}

// wrapWords breaks text into lines of at most width runes at spaces (a word
// longer than width gets its own line, which boxRow then ellipsises).
func wrapWords(text string, width int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(text) {
		if cur != "" && len([]rune(cur))+1+len([]rune(w)) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		if cur == "" {
			cur = w
		} else {
			cur += " " + w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func ledgerCodeSuffix(l LedgerStatus) string {
	if l.State == "disabled" && l.Code != "" {
		return " (" + l.Code + ")"
	}
	return ""
}

func (s *Status) printJSON(status SystemStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(s.out(), string(data))
	return err
}

// ════════════════════════════════════════════════════════════════════════════
// UTILITIES
// ════════════════════════════════════════════════════════════════════════════

func statusSymbolInline(status string) string {
	switch status {
	case "healthy":
		return "✓"
	case "unhealthy":
		return "✗"
	case "disabled":
		return "-"
	case "warning":
		return "⚠"
	default:
		return "?"
	}
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
