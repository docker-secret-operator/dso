package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/internal/webui"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func NewUICmd() *cobra.Command {
	var (
		port        int
		apiAddr     string
		openBrowser bool
	)

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the DSO web dashboard",
		Long: `Start the DSO web dashboard on a configurable port.

The dashboard provides a web-based interface to monitor and manage DSO agent status,
secrets, events, audit logs, and system configuration.

The dashboard reverse-proxies API requests to the DSO REST API, allowing it to
communicate with the running agent without additional configuration.

Examples:
  dso ui                        # Start dashboard on :8472
  dso ui --port 3000            # Start dashboard on :3000
  dso ui --api http://myapi:8471 # Use different API server
  dso ui --open-browser         # Automatically open dashboard in browser`,

		Run: func(cmd *cobra.Command, args []string) {
			logger, err := observability.NewLogger("info", "console", false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
				os.Exit(1)
			}
			defer logger.Sync()

			// Validate port range
			if port < 1024 || port > 65535 {
				fmt.Fprintf(os.Stderr, "Invalid port: %d (must be 1024-65535)\n", port)
				os.Exit(1)
			}

			// Format address
			addr := fmt.Sprintf(":%d", port)

			// Check if port is available
			if !webui.IsPortAvailable(port) {
				fmt.Fprintf(os.Stderr, "Port %d is already in use\n", port)
				os.Exit(1)
			}

			// Create server
			logger.Info("Initializing dashboard",
				zap.String("addr", addr),
				zap.String("api_target", apiAddr))

			srv, err := webui.NewServer(addr, apiAddr, logger)
			if err != nil {
				logger.Fatal("Failed to create dashboard server", zap.Error(err))
			}

			// Print startup message
			dashboardURL := webui.GetURLForPort(port)
			fmt.Printf("🚀 Dashboard starting on %s\n", dashboardURL)
			fmt.Printf("📊 API server: %s\n", apiAddr)
			fmt.Printf("Press Ctrl+C to stop\n\n")

			// Try to open browser if requested
			if openBrowser {
				logger.Debug("Opening browser", zap.String("url", dashboardURL))
				openBrowserURL(dashboardURL, logger)
			}

			// Setup signal handling for graceful shutdown
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

			// Start server in background
			errChan := make(chan error, 1)
			go func() {
				errChan <- srv.Listen()
			}()

			// Wait for shutdown signal or server error
			select {
			case <-sigChan:
				fmt.Println("\n⏹️  Shutting down dashboard...")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := srv.Shutdown(ctx); err != nil {
					logger.Error("Shutdown error", zap.Error(err))
					os.Exit(1)
				}
				fmt.Println("✅ Dashboard stopped")

			case err := <-errChan:
				if err != nil {
					logger.Fatal("Server error", zap.Error(err))
				}
			}
		},
	}

	cmd.Flags().IntVar(&port, "port", 8472, "Port to listen on")
	cmd.Flags().StringVar(&apiAddr, "api", "http://127.0.0.1:8471", "DSO REST API address")
	cmd.Flags().BoolVar(&openBrowser, "open-browser", false, "Open dashboard in browser (if xdg-open or open available)")

	return cmd
}

// openBrowserURL opens url in the default browser. Runs non-blocking; logs failures but does not crash.
func openBrowserURL(url string, logger *zap.Logger) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		logger.Debug("Browser auto-open not supported on this OS", zap.String("goos", runtime.GOOS))
		return
	}
	if err := cmd.Start(); err != nil {
		logger.Debug("Failed to open browser", zap.String("url", url), zap.Error(err))
		return
	}
	// Detach — do not wait; let the process run independently
	go func() { _ = cmd.Wait() }()
}
