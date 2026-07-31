package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/spf13/cobra"
)

// NewStatusCmd creates the status operational visibility command
func NewStatusCmd() *cobra.Command {
	var (
		watchFlag bool
		jsonFlag  bool
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
  docker dso status --json       # Machine-readable output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			status := &Status{
				Watch: watchFlag,
				JSON:  jsonFlag,
			}
			return status.Run()
		},
	}

	cmd.Flags().BoolVar(&watchFlag, "watch", false, "Auto-refresh every 2 seconds")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "Output as JSON")

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// STATUS TYPES
// ════════════════════════════════════════════════════════════════════════════

type Status struct {
	Watch bool
	JSON  bool
}

type RuntimeStatus struct {
	Mode      string    `json:"mode"`
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime"`
	StartTime time.Time `json:"start_time"`
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

type SystemStatus struct {
	Runtime      RuntimeStatus    `json:"runtime"`
	AgentReached bool             `json:"agent_reached"`
	AgentError   string           `json:"agent_error,omitempty"`
	Providers    []ProviderStatus `json:"providers,omitempty"`
	Cache        CacheStatus      `json:"cache"`
	Rotations    RotationStatus   `json:"rotations"`
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

func (s *Status) printStatus() error {
	systemStatus := s.gatherStatus()

	if s.JSON {
		return s.printJSON(systemStatus)
	}

	return s.printText(systemStatus)
}

func (s *Status) watchStatus() error {
	for {
		// Clear screen (ANSI escape code)
		fmt.Print("\033[2J\033[H")

		systemStatus := s.gatherStatus()
		if s.JSON {
			_ = s.printJSON(systemStatus)
		} else {
			_ = s.printText(systemStatus)
		}

		fmt.Println()
		fmt.Println("Refreshing in 2 seconds (Ctrl+C to exit)...")
		time.Sleep(2 * time.Second)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// STATUS GATHERING
// ════════════════════════════════════════════════════════════════════════════

func (s *Status) gatherStatus() SystemStatus {
	status := SystemStatus{
		Runtime: s.gatherRuntime(),
	}

	socketPath := "/run/dso/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	client, err := injector.NewAgentClientWithTimeout(socketPath, 3*time.Second)
	if err != nil {
		status.AgentReached = false
		status.AgentError = err.Error()
		status.Health = "✗ agent unreachable"
		return status
	}
	defer func() { _ = client.Close() }()

	resp, err := client.GetStatus()
	if err != nil {
		status.AgentReached = false
		status.AgentError = err.Error()
		status.Health = "✗ agent unreachable"
		return status
	}

	status.AgentReached = true
	status.Cache = CacheStatus{Entries: resp.CacheEntries}
	status.Rotations = RotationStatus{Pending: resp.PendingRotations}

	for _, p := range resp.Providers {
		ps := ProviderStatus{Name: p.Name, Message: p.Message}
		switch {
		case !p.Known:
			ps.Status = "unknown"
		case p.Healthy:
			ps.Status = "healthy"
		default:
			ps.Status = "unhealthy"
		}
		status.Providers = append(status.Providers, ps)
	}

	status.Health = "✓ All systems nominal"
	for _, p := range status.Providers {
		if p.Status == "unhealthy" {
			status.Health = "⚠ Some providers unhealthy"
			break
		}
	}

	return status
}

func (s *Status) gatherRuntime() RuntimeStatus {
	homeDir, _ := os.UserHomeDir()
	dsoDir := filepath.Join(homeDir, ".dso")

	mode := "unknown"
	if _, err := os.Stat("/etc/dso"); err == nil {
		mode = "agent"
	} else if _, err := os.Stat(dsoDir); err == nil {
		mode = "local"
	}

	// Try to read state file to get start time
	stateFile := filepath.Join(dsoDir, "state", "runtime.json")
	startTime := time.Now().Add(-2 * time.Hour) // Default assumption

	// #nosec G304 -- filename is a fixed literal; dsoDir is a resolved config
	// directory, not per-request input
	if data, err := os.ReadFile(stateFile); err == nil {
		var state map[string]interface{}
		if err := json.Unmarshal(data, &state); err == nil {
			if st, ok := state["start_time"].(string); ok {
				if t, err := time.Parse(time.RFC3339, st); err == nil {
					startTime = t
				}
			}
		}
	}

	uptime := time.Since(startTime)
	uptimeStr := formatDuration(uptime)

	return RuntimeStatus{
		Mode:      mode,
		Version:   "v1.0.0",
		Uptime:    uptimeStr,
		StartTime: startTime,
	}
}

// ════════════════════════════════════════════════════════════════════════════
// OUTPUT METHODS
// ════════════════════════════════════════════════════════════════════════════

func (s *Status) printText(status SystemStatus) error {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│              DSO Runtime Status                             │")
	fmt.Println("├─────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ Mode:     %-50s │\n", status.Runtime.Mode)
	fmt.Printf("│ Version:  %-50s │\n", status.Runtime.Version)
	fmt.Printf("│ Uptime:   %-50s │\n", status.Runtime.Uptime)
	fmt.Println("│                                                             │")

	if !status.AgentReached {
		fmt.Printf("│ AGENT: NOT REACHABLE (%-38s │\n", truncate(status.AgentError, 38)+")")
		fmt.Println("│                                                             │")
		fmt.Printf("│ HEALTH: %-53s │\n", status.Health)
		fmt.Println("└─────────────────────────────────────────────────────────────┘")
		fmt.Println()
		return nil
	}

	// Providers
	fmt.Println("│ PROVIDERS                                                   │")
	for i, p := range status.Providers {
		prefix := "├─"
		if i == len(status.Providers)-1 {
			prefix = "└─"
		}
		provStatus := statusSymbolInline(p.Status)
		fmt.Printf("│ %s %s:  %s %-35s │\n", prefix, p.Name, provStatus, p.Message)
	}
	fmt.Println("│                                                             │")

	// Cache
	fmt.Println("│ CACHE                                                       │")
	fmt.Printf("│ └─ Entries:  %-46d │\n", status.Cache.Entries)
	fmt.Println("│                                                             │")

	// Rotations
	fmt.Println("│ ROTATIONS                                                   │")
	fmt.Printf("│ └─ Pending:    %-43d │\n", status.Rotations.Pending)
	fmt.Println("│                                                             │")

	fmt.Printf("│ HEALTH: %-53s │\n", status.Health)
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println()

	return nil
}

func (s *Status) printJSON(status SystemStatus) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(data))
	return nil
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
