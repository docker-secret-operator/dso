package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			parent := cmd.Context()
			if parent == nil {
				parent = context.Background()
			}
			ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if useJournald || isSystemdAvailable() {
				return runJournaldLogs(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), follow, since, tail, level)
			}
			// Fallback to REST API events
			return runAPILogs(ctx, cmd.OutOrStdout(), apiAddr, follow)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output in real-time")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp or duration (e.g. '10 minutes ago', '2026-04-07 10:00:00')")
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, "Number of lines to show from the end of the logs")
	cmd.Flags().StringVar(&level, "level", "", "Filter by log level: debug, info, warn, error, fatal")
	cmd.Flags().StringVar(&apiAddr, "api-addr", "http://localhost:8471", "Agent REST API address (used when journald unavailable)")
	cmd.Flags().BoolVar(&useJournald, "api", false, "Use the agent REST API instead of journald")

	return cmd
}

// isSystemdAvailable checks if journalctl is available on the system
func isSystemdAvailable() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}

// runJournaldLogs reads DSO agent logs from the systemd journal. Ctrl+C (ctx
// cancellation) while following is a normal way to stop and returns nil.
func runJournaldLogs(ctx context.Context, out, errOut io.Writer, follow bool, since string, tail int, level string) error {
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

	printHeaderTo(out)

	journalCmd := exec.CommandContext(ctx, "journalctl", flatArgs...) // #nosec G204 -- fixed binary name; arguments are built from validated flags, not a shell
	journalCmd.Stdout = &logColorWriter{writer: out, levelFilter: strings.ToUpper(level)}
	journalCmd.Stderr = errOut

	if err := journalCmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil // interrupted by the operator
		}
		return fmt.Errorf("reading the journal failed: %w\nTip: try 'sudo docker dso logs' for full journal access, or 'docker dso logs --api' to read from the REST API", err)
	}
	return nil
}

// runAPILogs reads events from the DSO Agent REST API.
func runAPILogs(ctx context.Context, out io.Writer, apiAddr string, follow bool) error {
	if err := validateLoopbackURL(apiAddr); err != nil {
		return err
	}

	printHeaderTo(out)

	url := apiAddr + "/api/events"
	events, err := fetchEventsErr(ctx, url)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintln(out, stripIfNoColor("\033[33m[DSO] No events found. Is the agent running? Try: sudo systemctl start dso-agent\033[0m"))
	}
	for _, ev := range events {
		printEventTo(out, ev)
	}

	if !follow {
		return nil
	}

	fmt.Fprintln(out, stripIfNoColor("\033[36m[DSO] Following live events via REST API... (Ctrl+C to stop)\033[0m"))
	fmt.Fprintln(out, "--------------------------------------------------------------------------------")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	seen := make(map[string]bool)
	for _, ev := range events {
		seen[fmt.Sprintf("%v", ev)] = true
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			evs, err := fetchEventsErr(ctx, url)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			for _, ev := range evs {
				key := fmt.Sprintf("%v", ev)
				if !seen[key] {
					seen[key] = true
					printEventTo(out, ev)
				}
			}
		}
	}
}

func printHeader() { printHeaderTo(os.Stdout) }

func printHeaderTo(w io.Writer) {
	fmt.Fprintln(w, stripIfNoColor("\033[1;36m"))
	fmt.Fprintln(w, "  ╔══════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "  ║         Docker Secret Operator — Logs            ║")
	fmt.Fprintln(w, "  ╚══════════════════════════════════════════════════╝")
	fmt.Fprintln(w, stripIfNoColor("\033[0m"))
}

// validateLoopbackURL rejects any --api-addr value whose host resolves to a
// non-loopback address. The DSO agent only listens locally, so permitting
// arbitrary hosts would enable SSRF against internal network services.
func validateLoopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid API address %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid API address %q: scheme must be http or https", raw)
	}
	host := u.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve API address host %q: %w", host, err)
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("API address %q resolves to non-loopback IP %s: only loopback addresses are allowed", raw, ipStr)
		}
	}
	return nil
}

// fetchEvents is the best-effort form used where an error is not actionable:
// it returns nil on any failure. Commands use fetchEventsErr.
func fetchEvents(url string) []map[string]interface{} {
	ev, _ := fetchEventsErr(context.Background(), url)
	return ev
}

// fetchEventsErr fetches agent events and reports every failure to the caller
// instead of printing it.
func fetchEventsErr(ctx context.Context, url string) ([]map[string]interface{}, error) {
	// #nosec G107 -- every caller of fetchEventsErr passes a URL built from
	// apiAddr after runAPILogs has already called validateLoopbackURL,
	// which resolves the host and rejects anything but a loopback address.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid API request: %w", err)
	}
	// The agent REST API requires credentials whenever DSO_AUTH_TOKEN is set
	// (and, since the WebUI auth fix, whenever the WebUI is enabled). Send the
	// same token the agent was started with if it is available here.
	if token := os.Getenv("DSO_AUTH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G107 -- URL validated as loopback above.
	if err != nil {
		return nil, fmt.Errorf("cannot reach agent API at %s: %w\nIs the agent running? Try: sudo systemctl start dso-agent", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("the agent API rejected the request (401 Unauthorized)\nSet DSO_AUTH_TOKEN to the agent's token and retry, or read events with: sudo docker dso logs")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading agent API response: %w", err)
	}

	var events []map[string]interface{}
	if err := json.Unmarshal(body, &events); err != nil {
		// QUAL-1: a malformed API response must not look like "no events".
		return nil, fmt.Errorf("malformed response from agent API at %s: %w", url, err)
	}
	return events, nil
}

func printEvent(ev map[string]interface{}) { printEventTo(os.Stdout, ev) }

func printEventTo(w io.Writer, ev map[string]interface{}) {
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
	fmt.Fprintln(w, stripIfNoColor(line))
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
		coloured := stripIfNoColor(colorizeLine(line))
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
