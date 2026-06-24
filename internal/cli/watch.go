package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/spf13/cobra"
)

func NewWatchCmd() *cobra.Command {
	var debug bool
	var strategy string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Real-time monitor of secret rotations and container lifecycles",
		Run: func(cmd *cobra.Command, args []string) {
			socketPath := DefaultSocketPath()
			if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
				socketPath = custom
			}

			// New Docker Watcher logic
			dw, err := watcher.NewDockerWatcher(debug)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}

			ctx := context.Background()
			msgCh, errCh := dw.Subscribe(ctx)

			// Legacy Agent connection for DSO local events
			client, agentErr := injector.NewAgentClient(socketPath)
			if client != nil {
				defer client.Close()
			}

			fmt.Println("\033[1;36mDSO Watcher Active\033[0m (Strategy: " + strategy + ") - Monitoring live container events...")
			fmt.Println("-----------------------------------------------------------------------------------")
			if agentErr != nil {
				fmt.Printf("\033[1;33m[WARN]\033[0m Agent not connected (%v)\n", agentErr)
				fmt.Println("       Start the agent with: sudo docker dso agent  (or: sudo systemctl start dso-agent)")
				fmt.Println("       DSO rotation events will not appear until the agent is running.")
				fmt.Println("-----------------------------------------------------------------------------------")
			} else {
				fmt.Println("\033[1;32m[OK]\033[0m   Agent connected — rotation events will appear below.")
				fmt.Println("-----------------------------------------------------------------------------------")
			}

			seenAgentMsgs := make(map[string]bool)

			for {
				select {
				case <-ctx.Done():
					return
				case dwErr := <-errCh:
					fmt.Fprintf(os.Stderr, "[ERROR] Docker event stream error: %v\n", dwErr)
					time.Sleep(2 * time.Second)
					msgCh, errCh = dw.Subscribe(ctx) // Try to reconnect
				case msg := <-msgCh:
					// Format and display Docker native events
					watcher.ProcessEvent(msg, debug)
				case <-time.After(1 * time.Second):
					// Periodically check Agent for DSO specific rotation events
					if client != nil {
						resp, err := client.GetEvents()
						if err == nil {
							for _, m := range resp.Data {
								if !seenAgentMsgs[m] {
									fmt.Printf("\033[1;32m[DSO ROTATION]\033[0m [%s] %s\n", time.Now().Format("15:04:05"), m)
									seenAgentMsgs[m] = true
								}
							}
						}
					}
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&debug, "debug", "d", false, "Enable raw event payload output")
	cmd.Flags().StringVar(&strategy, "strategy", "auto", "Rotation strategy (auto/rolling/restart)")

	return cmd
}
