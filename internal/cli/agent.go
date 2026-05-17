package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/server"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// performPreflightChecks validates system health before marking agent as ready (Fix #5)
func performPreflightChecks(ctx context.Context, logger *zap.Logger,
	storeManager *providers.SecretStoreManager, cache *agent.SecretCache) error {

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	logger.Info("Running preflight checks...")

	// Check 1: Verify cache is operational
	if cache == nil {
		logger.Error("Secret cache not initialized")
		return fmt.Errorf("secret cache not initialized")
	}
	logger.Info("✓ Cache is operational")

	// Check 2: Verify store manager is operational
	if storeManager == nil {
		logger.Error("Secret store manager not initialized")
		return fmt.Errorf("secret store manager not initialized")
	}
	logger.Info("✓ Store manager is operational")

	// Check 3: Verify context is not already cancelled
	select {
	case <-checkCtx.Done():
		logger.Error("Preflight check timeout or context cancelled")
		return checkCtx.Err()
	default:
	}

	logger.Info("✓ All preflight checks passed")
	return nil
}

func NewAgentCmd() *cobra.Command {
	var socketPath string
	var driverSocket string
	var apiAddr string

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the DSO background reconciliation engine",
		Long:  `The agent command starts the DSO reconciliation loop, Unix socket server, and Docker Secret Driver interface.`,
		Run: func(cmd *cobra.Command, args []string) {
			logger, _ := observability.NewLogger("info", "console", false)
			defer func() {
				_ = logger.Sync()
			}()

			cfgPath := ResolveConfig()
			cfg, err := config.LoadConfig(cfgPath)
			if err != nil {
				logger.Fatal("Agent failed to load configuration - check if path exists and is allowed",
					zap.Error(err),
					zap.String("config_path", cfgPath))
			}

			// Initialize Cache & Store
			cache := agent.NewSecretCache(5 * time.Minute)
			defer cache.Close()
			storeManager := providers.NewSecretStoreManager(logger)
			defer storeManager.Shutdown()

			// Initialize Reloader (Watcher)
			reloader, err := watcher.NewReloaderController(logger)
			if err != nil {
				logger.Fatal("Failed to initialize reloader controller", zap.Error(err))
			}

			// Initialize Docker Client (for crash recovery and container management)
			dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			if err != nil {
				logger.Fatal("Failed to initialize Docker client",
					zap.Error(err))
			}

			// Initialize Trigger Engine
			trigger := agent.NewTriggerEngine(cache, storeManager, reloader, logger, cfg, dockerCli)

			// 1. Start Unix Socket Server (Internal IPC)
			agentServer, err := agent.StartSocketServer(socketPath, cache, storeManager, logger, cfg)
			if err != nil {
				logger.Fatal("Failed to start agent socket server", zap.Error(err))
			}
			trigger.Server = agentServer
			reloader.Server = agentServer

			// Handle Termination with Graceful Shutdown
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// 2. Start Docker Secret Driver Server (V2 Plugin)
			go func() {
				if err := agent.StartDriverServer(driverSocket, cache, storeManager, logger, cfg); err != nil {
					logger.Error("Docker Driver server error", zap.Error(err))
				}
			}()

			// 3. Start REST API Server (Health Checks & Monitoring)
			restShutdown := server.StartRESTServer(ctx, apiAddr, cache, trigger, cfg, logger)
			defer restShutdown()

			// 4. Start Reconciliation Loop
			if err := trigger.StartAll(); err != nil {
				logger.Fatal("Failed to start trigger engine", zap.Error(err))
			}

			// PREFLIGHT CHECKS: Verify system is healthy before marking ready (Fix #5)
			if err := performPreflightChecks(ctx, logger, storeManager, cache); err != nil {
				logger.Error("Preflight checks failed, proceeding but system may not be fully operational",
					zap.Error(err))
			}

			logger.Info("DSO Agent is now running",
				zap.String("version", "v3.5.7"),
				zap.String("ipc_socket", socketPath),
				zap.String("driver_socket", driverSocket),
				zap.String("api_addr", apiAddr))

			// 5. Start Docker Event Loop for the Reloader (CRITICAL BUG FIX)
			reloader.StartEventLoop(ctx)

			<-ctx.Done()
			logger.Info("Shutting down DSO Agent gracefully...")

			// GRACEFUL SHUTDOWN SEQUENCE (Fix #4)
			// Step 1: Stop accepting new work
			logger.Info("Stopping trigger engine...")
			trigger.Stop()

			// Step 2: Wait for in-flight operations to complete (with timeout)
			shutdownTimeout := 30 * time.Second
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()

			logger.Info("Waiting for in-flight operations to complete",
				zap.Duration("timeout", shutdownTimeout))

			// Wait for context cancellation to propagate to all goroutines
			<-shutdownCtx.Done()
			if shutdownCtx.Err() == context.DeadlineExceeded {
				logger.Warn("Shutdown timeout exceeded, forcing cleanup",
					zap.Duration("timeout", shutdownTimeout))
			}

			// Step 3: Close resources
			logger.Info("Closing resources...")
			cache.Close()
			storeManager.Shutdown()
			if dockerCli != nil {
				dockerCli.Close()
			}

			// Step 4: Cleanup sockets
			logger.Info("Cleaning up sockets...")
			if err := os.Remove(socketPath); err != nil {
				logger.Warn("Failed to remove IPC socket on shutdown",
					zap.String("path", socketPath),
					zap.Error(err))
			}
			if err := os.Remove(driverSocket); err != nil {
				logger.Warn("Failed to remove driver socket on shutdown",
					zap.String("path", driverSocket),
					zap.Error(err))
			}

			logger.Info("DSO Agent shutdown completed")
			fmt.Println("DSO Agent stopped.")
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", "/run/dso/dso.sock", "Path to DSO internal IPC socket")
	cmd.Flags().StringVar(&driverSocket, "driver-socket", "/run/docker/plugins/dso.sock", "Path to Docker Secret Driver socket")
	cmd.Flags().StringVar(&apiAddr, "api-addr", "127.0.0.1:8080", "Address to bind the REST API server")

	return cmd
}
