package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func NewAgentCmd() *cobra.Command {
	var socketPath string
	var driverSocket string

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the DSO background reconciliation engine",
		Long:  `The agent command starts the DSO reconciliation loop, Unix socket server, and Docker Secret Driver interface.`,
		Run: func(cmd *cobra.Command, args []string) {
			logger, _ := observability.NewLogger("info", "console", false)
			defer logger.Sync()

			cfgPath := ResolveConfig()
			cfg, err := config.LoadConfig(cfgPath)
			if err != nil {
				logger.Fatal("Failed to load configuration", zap.Error(err), zap.String("path", cfgPath))
			}

			// Initialize Cache & Store
			cache := agent.NewSecretCache(5 * time.Minute)
			storeManager := providers.NewSecretStoreManager(logger)
			
			// Initialize Reloader (Watcher)
			reloader, err := watcher.NewReloaderController(logger)
			if err != nil {
				logger.Fatal("Failed to initialize reloader controller", zap.Error(err))
			}
			
			// Initialize Trigger Engine
			trigger := agent.NewTriggerEngine(cache, storeManager, reloader, logger, cfg)

			// 1. Start Unix Socket Server (Internal IPC)
			agentServer, err := agent.StartSocketServer(socketPath, cache, storeManager, logger, cfg)
			if err != nil {
				logger.Fatal("Failed to start agent socket server", zap.Error(err))
			}
			trigger.Server = agentServer

			// 2. Start Docker Secret Driver Server (V2 Plugin)
			go func() {
				if err := agent.StartDriverServer(driverSocket, cache, storeManager, logger, cfg); err != nil {
					logger.Error("Docker Driver server error", zap.Error(err))
				}
			}()

			// 3. Start Reconciliation Loop
			if err := trigger.StartAll(); err != nil {
				logger.Fatal("Failed to start trigger engine", zap.Error(err))
			}

			logger.Info("DSO Agent is now running", 
				zap.String("version", "v3.1.0"),
				zap.String("ipc_socket", socketPath),
				zap.String("driver_socket", driverSocket))

			// Handle Termination
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			
			sig := <-sigChan
			logger.Info("Shutting down DSO Agent", zap.String("signal", sig.String()))
			
			// Stop components
			trigger.Stop()
			
			// Cleanup sockets
			_ = os.Remove(socketPath)
			_ = os.Remove(driverSocket)
			
			fmt.Println("DSO Agent stopped.")
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", "/var/run/dso.sock", "Path to DSO internal IPC socket")
	cmd.Flags().StringVar(&driverSocket, "driver-socket", "/run/docker/plugins/dso.sock", "Path to Docker Secret Driver socket")

	return cmd
}
