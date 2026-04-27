package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/core"
	"github.com/docker-secret-operator/dso/internal/resolver"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ensureAgentRunning triggers the legacy background agent for Cloud Mode
func ensureAgentRunning(configPath string) {
	socketPath := "/var/run/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err == nil {
		conn.Close()
		return
	}

	fmt.Println("🚀 Starting DSO agent...")
	args := []string{"agent"}
	if configPath != "" && configPath != "dso.yaml" {
		args = append(args, "--config", configPath)
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start DSO agent: %v\n", err)
		return
	}

	start := time.Now()
	for time.Since(start) < 5*time.Second {
		conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "Warning: Agent started but socket %s not ready yet.\n", socketPath)
}

func detectMode(flagMode string, configPath string) string {
	if flagMode != "" {
		return strings.ToLower(flagMode)
	}

	// Priority 1: Check for Native Vault. If it exists, default to LOCAL mode.
	home, _ := os.UserHomeDir()
	vaultPath := filepath.Join(home, ".dso", "vault.enc")
	if _, err := os.Stat(vaultPath); err == nil {
		return "local"
	}

	// Priority 2: Check for global Cloud configuration.
	if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
		return "cloud"
	}

	// Default to local (it will error later if no vault or secrets found)
	return "local"
}

func getProjectName(args []string) string {
	for i, arg := range args {
		if arg == "-p" || arg == "--project-name" {
			if i+1 < len(args) {
				return args[i+1]
			}
		}
		if strings.HasPrefix(arg, "--project-name=") {
			return strings.TrimPrefix(arg, "--project-name=")
		}
	}
	
	if envName := os.Getenv("COMPOSE_PROJECT_NAME"); envName != "" {
		return envName
	}
	
	dir, err := os.Getwd()
	if err == nil {
		return filepath.Base(dir)
	}
	return "default"
}

func NewUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "up [args...]",
		Short:              "Deploy a stack and automatically start the DSO agent",
		Long:               "The 'up' command is the primary entrypoint for DSO. It performs a Docker Compose deployment with secure secret injection.",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			composeFile := ""
			flagMode := ""
			configPath := extractConfigFromArgs(os.Args)
			if configPath == "" {
				configPath = ResolveConfig()
			}

			var dockerArgs []string
			dryRun := false

			for i := 0; i < len(args); i++ {
				arg := args[i]
				
				if arg == "--help" || arg == "-h" {
					cmd.Help()
					return
				}
				if strings.HasPrefix(arg, "--mode=") {
					flagMode = strings.TrimPrefix(arg, "--mode=")
					continue
				}
				if arg == "-f" || arg == "--file" || arg == "-cf" {
					if i+1 < len(args) {
						composeFile = args[i+1]
						i++ // Skip the value in the next iteration
					}
					continue
				}
				if strings.HasPrefix(arg, "--file=") || strings.HasPrefix(arg, "-cf=") {
					composeFile = strings.TrimPrefix(strings.TrimPrefix(arg, "--file="), "-cf=")
					continue
				}
				if arg == "--config" || arg == "-c" {
					i++ // Skip value
					continue
				}
				if strings.HasPrefix(arg, "--config=") {
					continue
				}
				if arg == "--dry-run" {
					dryRun = true
					dockerArgs = append(dockerArgs, arg)
					continue
				}
				if arg == "--debug" {
					core.SetDebug(true)
					continue
				}
				dockerArgs = append(dockerArgs, arg)
			}

			if composeFile == "" {
				if _, err := os.Stat("docker-compose.yml"); err == nil {
					composeFile = "docker-compose.yml"
				} else if _, err := os.Stat("docker-compose.yaml"); err == nil {
					composeFile = "docker-compose.yaml"
				} else {
					fmt.Fprintln(os.Stderr, "Error: No docker-compose.yml found.")
					os.Exit(1)
				}
			}

			mode := detectMode(flagMode, configPath)

			if mode == "cloud" {
				fmt.Printf("🔐 DSO Mode: CLOUD (%s)\n", configPath)

				content, err := os.ReadFile(composeFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading compose file: %v\n", err)
					os.Exit(1)
				}

				if strings.Contains(string(content), "dsofile://") {
					fmt.Fprintln(os.Stderr, "Error: dsofile:// protocol is only supported in LOCAL mode. Please migrate to the Native Vault.")
					os.Exit(1)
				}

				ensureAgentRunning(configPath)
				err = core.RunComposeUpWithEnv(composeFile, dockerArgs, configPath, dryRun)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error running up: %v\n", err)
					os.Exit(1)
				}
			} else {
				fmt.Println("🔐 DSO Mode: LOCAL (Native Vault)")
				fmt.Println("🔐 Resolving secrets...")

				v, err := vault.LoadDefault()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error loading native vault: %v\n", err)
					os.Exit(1)
				}

				fmt.Println("🚀 Starting DSO agent...")
				cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error connecting to Docker API: %v\n", err)
					os.Exit(1)
				}

				content, err := os.ReadFile(composeFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading compose file: %v\n", err)
					os.Exit(1)
				}

				var root yaml.Node
				if err := yaml.Unmarshal(content, &root); err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing YAML AST: %v\n", err)
					os.Exit(1)
				}

				projectName := getProjectName(dockerArgs)
				ctx := context.Background()
				mutatedRoot, seed, err := resolver.ResolveCompose(ctx, cli, &root, v, projectName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error resolving compose secrets: %v\n", err)
					os.Exit(1)
				}

				agentDaemon := agent.NewAgent(cli)
				agentDaemon.GetCache().Seed(seed)

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				go func() {
					if err := agentDaemon.Start(ctx); err != nil {
						fmt.Fprintf(os.Stderr, "Agent error: %v\n", err)
					}
				}()

				// Wait for agent to be ready (listening for events)
				select {
				case <-agentDaemon.Ready:
					// Ready to proceed
				case <-time.After(5 * time.Second):
					fmt.Fprintln(os.Stderr, "Warning: Agent startup timed out, proceeding anyway...")
				}

				tmpFile, err := os.CreateTemp("", "docker-compose-dso-*.yaml")
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating temp file: %v\n", err)
					os.Exit(1)
				}
				defer os.Remove(tmpFile.Name())

				enc := yaml.NewEncoder(tmpFile)
				enc.SetIndent(2)
				if err := enc.Encode(mutatedRoot); err != nil {
					fmt.Fprintf(os.Stderr, "Error writing AST mutation: %v\n", err)
					os.Exit(1)
				}
				enc.Close()

				fmt.Println("🐳 Running docker compose...")
				
				// Reconstruct docker-compose command with the mutated AST file.
				// We MUST explicitly pass the project name because using a temp file
				// in /tmp would otherwise cause Docker to derive the wrong project name.
				execArgs := append([]string{"compose", "-p", projectName, "-f", tmpFile.Name(), "up"}, dockerArgs...)
				execCmd := exec.Command("docker", execArgs...)
				execCmd.Stdout = os.Stdout
				execCmd.Stderr = os.Stderr
				execCmd.Stdin = os.Stdin
				
				if err := execCmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "Docker compose failed: %v\n", err)
					os.Exit(1)
				}
			}
		},
	}
}
