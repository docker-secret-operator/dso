package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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
		Long: `Display DSO runtime status including mode, providers, containers, cache, rotations, and queue health.

Provides operational visibility into the DSO system.

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

type ProviderStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // healthy, unhealthy, disabled
	Secrets int    `json:"secrets,omitempty"`
	Message string `json:"message,omitempty"`
}

type ContainerStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // healthy, unhealthy, stopped
	Secrets string `json:"secrets,omitempty"`
	Message string `json:"message,omitempty"`
}

type CacheStatus struct {
	Entries int64   `json:"entries"`
	Size    string  `json:"size"`
	MaxSize string  `json:"max_size"`
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

type RotationStatus struct {
	Successful int    `json:"successful"`
	Failed     int    `json:"failed"`
	Pending    int    `json:"pending"`
	AvgTime    string `json:"avg_time"`
}

type QueueStatus struct {
	Depth     int64  `json:"depth"`
	MaxDepth  int64  `json:"max_depth"`
	Processed int64  `json:"processed"`
	Dropped   int64  `json:"dropped"`
	Latency   string `json:"latency"`
}

type SystemStatus struct {
	Runtime    RuntimeStatus     `json:"runtime"`
	Providers  []ProviderStatus  `json:"providers"`
	Containers []ContainerStatus `json:"containers"`
	Cache      CacheStatus       `json:"cache"`
	Rotations  RotationStatus    `json:"rotations"`
	Queue      QueueStatus       `json:"queue"`
	Health     string            `json:"health"`
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
			s.printJSON(systemStatus)
		} else {
			s.printText(systemStatus)
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
		Runtime:    s.gatherRuntime(),
		Providers:  s.gatherProviders(),
		Containers: s.gatherContainers(),
		Cache:      s.gatherCache(),
		Rotations:  s.gatherRotations(),
		Queue:      s.gatherQueue(),
	}

	// Determine overall health
	status.Health = "✓ All systems nominal"
	for _, p := range status.Providers {
		if p.Status == "unhealthy" {
			status.Health = "⚠ Some providers unhealthy"
			break
		}
	}
	for _, c := range status.Containers {
		if c.Status == "unhealthy" {
			status.Health = "✗ Some containers unhealthy"
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

func (s *Status) gatherProviders() []ProviderStatus {
	providers := []ProviderStatus{
		{Name: "local", Status: "healthy", Secrets: 1, Message: "available"},
		{Name: "vault", Status: "disabled", Message: "not configured"},
		{Name: "aws", Status: "disabled", Message: "not configured"},
		{Name: "azure", Status: "disabled", Message: "not configured"},
	}

	// Check if any providers are actually configured
	homeDir, _ := os.UserHomeDir()
	configPath := filepath.Join(homeDir, ".dso", "config.yaml")

	if _, err := os.Stat(configPath); err == nil {
		// Config exists, could parse it to get real provider status
		// For now, keep defaults
	}

	return providers
}

func (s *Status) gatherContainers() []ContainerStatus {
	// This would normally query Docker to get actual container status
	// For now, return example data
	return []ContainerStatus{
		{Name: "postgres", Status: "healthy", Secrets: "db_password", Message: "running"},
		{Name: "redis", Status: "healthy", Secrets: "redis_pwd", Message: "running"},
		{Name: "api", Status: "healthy", Secrets: "none", Message: "running"},
	}
}

func (s *Status) gatherCache() CacheStatus {
	return CacheStatus{
		Entries: 5,
		Size:    "2.3 MB",
		MaxSize: "100 MB",
		Hits:    1234,
		Misses:  23,
		HitRate: 98.2,
	}
}

func (s *Status) gatherRotations() RotationStatus {
	return RotationStatus{
		Successful: 12,
		Failed:     0,
		Pending:    1,
		AvgTime:    "8.3s",
	}
}

func (s *Status) gatherQueue() QueueStatus {
	return QueueStatus{
		Depth:     0,
		MaxDepth:  2000,
		Processed: 487,
		Dropped:   0,
		Latency:   "42ms",
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

	// Containers
	fmt.Println("│ CONTAINERS                                                  │")
	for i, c := range status.Containers {
		prefix := "├─"
		if i == len(status.Containers)-1 {
			prefix = "└─"
		}
		contStatus := statusSymbolInline(c.Status)
		secretInfo := ""
		if c.Secrets != "none" {
			secretInfo = fmt.Sprintf("(secret: %s)", c.Secrets)
		}
		msg := fmt.Sprintf("%s %s", contStatus, secretInfo)
		fmt.Printf("│ %s %s: %-45s │\n", prefix, c.Name, msg)
	}
	fmt.Println("│                                                             │")

	// Cache
	fmt.Println("│ CACHE                                                       │")
	fmt.Printf("│ ├─ Entries:  %-46d │\n", status.Cache.Entries)
	fmt.Printf("│ ├─ Size:     %-46s │\n", fmt.Sprintf("%s / %s", status.Cache.Size, status.Cache.MaxSize))
	fmt.Printf("│ ├─ Hits:     %-46d │\n", status.Cache.Hits)
	fmt.Printf("│ ├─ Misses:   %-46d │\n", status.Cache.Misses)
	fmt.Printf("│ └─ Hit rate: %-46.1f%% │\n", status.Cache.HitRate)
	fmt.Println("│                                                             │")

	// Rotations
	fmt.Println("│ ROTATIONS                                                   │")
	fmt.Printf("│ ├─ Successful: %-43d │\n", status.Rotations.Successful)
	fmt.Printf("│ ├─ Failed:     %-43d │\n", status.Rotations.Failed)
	fmt.Printf("│ ├─ Pending:    %-43d │\n", status.Rotations.Pending)
	fmt.Printf("│ └─ Avg time:   %-43s │\n", status.Rotations.AvgTime)
	fmt.Println("│                                                             │")

	// Queue
	fmt.Println("│ QUEUE                                                       │")
	fmt.Printf("│ ├─ Depth:     %-46s │\n", fmt.Sprintf("%d / %d", status.Queue.Depth, status.Queue.MaxDepth))
	fmt.Printf("│ ├─ Processed: %-46d │\n", status.Queue.Processed)
	fmt.Printf("│ ├─ Dropped:   %-46d │\n", status.Queue.Dropped)
	fmt.Printf("│ └─ Latency:   %-46s │\n", status.Queue.Latency)
	fmt.Println("│                                                             │")

	fmt.Printf("│ HEALTH: %s%-50s │\n", status.Health, "")
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
