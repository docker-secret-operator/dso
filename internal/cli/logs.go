package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// coloured log level formatting
var levelColors = map[string]string{
	"DEBUG": "\033[36m", // cyan
	"INFO":  "\033[32m", // green
	"WARN":  "\033[33m", // yellow
	"ERROR": "\033[31m", // red
	"FATAL": "\033[35m", // magenta
	"reset": "\033[0m",
}

func colorLevel(level string) string {
	upper := strings.ToUpper(level)
	if col, ok := levelColors[upper]; ok {
		return col + upper + levelColors["reset"]
	}
	return upper
}

func NewLogsCmd() *cobra.Command {
	var follow bool
	var since string
	var tail int
	var level string
	var apiAddr string
	var useJournald bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View DSO agent logs",
		Long: `View logs from the DSO Agent service.

By default, reads from the systemd journal (journald) if running as a service.
Use --api to stream live events from the agent REST API instead.

Examples:
  docker dso logs                          # Show last 100 lines
  docker dso logs -f                       # Follow live (tail -f style)
  docker dso logs -n 50                    # Show last 50 lines
  docker dso logs --since "10 minutes ago" # Logs from last 10 minutes
  docker dso logs --level error            # Filter to errors only
  docker dso logs --api                    # Stream from REST API`,
		Run: func(cmd *cobra.Command, args []string) {
			if useJournald || isSystemdAvailable() {
				runJournaldLogs(follow, since, tail, level)
			} else {
				// Fallback to REST API events
				runAPILogs(apiAddr, follow)
			}
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output in real-time")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp or duration (e.g. '10 minutes ago', '2026-04-07 10:00:00')")
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, "Number of lines to show from the end of the logs")
	cmd.Flags().StringVar(&level, "level", "", "Filter by log level: debug, info, warn, error, fatal")
	cmd.Flags().StringVar(&apiAddr, "api-addr", "http://localhost:8080", "Agent REST API address (used when journald unavailable)")
	cmd.Flags().BoolVar(&useJournald, "api", false, "Use the agent REST API instead of journald")

	return cmd
}

// isSystemdAvailable checks if journalctl is available on the system
func isSystemdAvailable() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}

// runJournaldLogs reads DSO agent logs from the systemd journal
func runJournaldLogs(follow bool, since string, tail int, level string) {
	args := []string{
		"-u", "dso-agent",
		"-l", // full output, no ellipsis
		"--no-pager",
		fmt.Sprintf("-n %d", tail),
	}

	if follow {
		args = append(args, "-f")
	}
	if since != "" {
		args = append(args, "--since", since)
	}

	// Map log level to journald priority
	if level != "" {
		priority := journaldPriority(level)
		if priority != "" {
			args = append(args, "-p", priority)
		}
	}

	// Flatten args (some entries already have spaces)
	var flatArgs []string
	for _, a := range args {
		flatArgs = append(flatArgs, strings.Fields(a)...)
	}

	printHeader()

	journalCmd := exec.Command("journalctl", flatArgs...) // #nosec G204 — args are controlled internally
	journalCmd.Stdout = &logColorWriter{writer: os.Stdout, levelFilter: strings.ToUpper(level)}
	journalCmd.Stderr = os.Stderr

	if err := journalCmd.Run(); err != nil {
		// If journalctl fails (permissions), suggest sudo
		fmt.Fprintf(os.Stderr, "\n\033[33m[DSO] Tip: Try running with 'sudo docker dso logs' for full journal access.\033[0m\n")
		fmt.Fprintf(os.Stderr, "\033[33m      Or use 'docker dso logs --api' to read from the REST API instead.\033[0m\n")
	}
}

// runAPILogs reads events from the DSO Agent REST API
func runAPILogs(apiAddr string, follow bool) {
	printHeader()

	url := apiAddr + "/api/events"
	fetchAndPrintEvents(url)

	if follow {
		fmt.Println("\033[36m[DSO] Following live events via REST API... (Ctrl+C to stop)\033[0m")
		fmt.Println("--------------------------------------------------------------------------------")
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		seen := make(map[string]bool)
		for range ticker.C {
			events := fetchEvents(url)
			for _, ev := range events {
				key := fmt.Sprintf("%v", ev)
				if !seen[key] {
					seen[key] = true
					printEvent(ev)
				}
			}
		}
	}
}

func printHeader() {
	fmt.Println("\033[1;36m")
	fmt.Println("  ╔══════════════════════════════════════════════════╗")
	fmt.Println("  ║         Docker Secret Operator — Logs            ║")
	fmt.Println("  ╚══════════════════════════════════════════════════╝")
	fmt.Println("\033[0m")
}

func fetchAndPrintEvents(url string) {
	events := fetchEvents(url)
	if len(events) == 0 {
		fmt.Println("\033[33m[DSO] No events found. Is the agent running? Try: sudo systemctl start dso-agent\033[0m")
		return
	}
	for _, ev := range events {
		printEvent(ev)
	}
}

func fetchEvents(url string) []map[string]interface{} {
	resp, err := http.Get(url) // #nosec G107 — URL is constructed from a flag value
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m[DSO] Cannot reach agent API at %s: %v\033[0m\n", url, err)
		fmt.Fprintf(os.Stderr, "\033[33m      Is the agent running? Try: sudo systemctl start dso-agent\033[0m\n")
		return nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var events []map[string]interface{}
	_ = json.Unmarshal(body, &events)
	return events
}

func printEvent(ev map[string]interface{}) {
	ts, _ := ev["timestamp"].(string)
	level, _ := ev["level"].(string)
	msg, _ := ev["message"].(string)
	secret, _ := ev["secret"].(string)

	if ts == "" {
		ts = time.Now().Format("15:04:05")
	}

	line := fmt.Sprintf("  \033[90m%s\033[0m  %s", ts, colorLevel(level))
	if secret != "" {
		line += fmt.Sprintf("  \033[36m[%s]\033[0m", secret)
	}
	line += "  " + msg
	fmt.Println(line)
}

// journaldPriority maps DSO log levels to journald priority numbers
func journaldPriority(level string) string {
	switch strings.ToLower(level) {
	case "debug":
		return "debug"
	case "info":
		return "info"
	case "warn", "warning":
		return "warning"
	case "error":
		return "err"
	case "fatal", "critical":
		return "crit"
	default:
		return ""
	}
}

// logColorWriter adds colour to journald output lines based on log level keywords
type logColorWriter struct {
	writer      io.Writer
	levelFilter string
}

func (w *logColorWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		coloured := colorizeLine(line)
		// Apply level filter if set
		if w.levelFilter != "" && !strings.Contains(strings.ToUpper(line), w.levelFilter) {
			continue
		}
		if _, err := fmt.Fprintln(w.writer, coloured); err != nil {
			return n, err
		}
	}
	return len(p), nil
}

func colorizeLine(line string) string {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "FATAL"):
		return levelColors["FATAL"] + line + levelColors["reset"]
	case strings.Contains(upper, "ERROR"):
		return levelColors["ERROR"] + line + levelColors["reset"]
	case strings.Contains(upper, "WARN"):
		return levelColors["WARN"] + line + levelColors["reset"]
	case strings.Contains(upper, "DEBUG"):
		return levelColors["DEBUG"] + line + levelColors["reset"]
	default:
		return line
	}
}
