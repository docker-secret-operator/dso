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

// checkCloudAgent verifies the systemd agent is running and responsive.
func checkCloudAgent() {
	socketPath := "/var/run/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err == nil {
		conn.Close()
		return
	}

	fmt.Fprintln(os.Stderr, "Error: Failed to connect to DSO background agent.")
	if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
		fmt.Fprintf(os.Stderr, "Reason: Permission denied accessing %s\n", socketPath)
		fmt.Fprintln(os.Stderr, "\nFix: Cloud mode requires elevated permissions to access the daemon.")
		fmt.Fprintln(os.Stderr, "Run next: sudo docker dso up")
	} else {
		fmt.Fprintf(os.Stderr, "Reason: Connection refused or socket %s is missing.\n", socketPath)
		fmt.Fprintln(os.Stderr, "\nFix: Cloud mode requires the systemd agent to be active.")
		fmt.Fprintln(os.Stderr, "Run next: sudo docker dso system setup")
		fmt.Fprintln(os.Stderr, "Diagnostics: docker dso system doctor")
	}
	os.Exit(1)
}

func detectMode(flagMode string, configPath string) (string, string) {
	if flagMode != "" {
		return strings.ToLower(flagMode), "flag"
	}

	if envMode := os.Getenv("DSO_MODE"); envMode != "" {
		return strings.ToLower(envMode), "env"
	}
	if envMode := os.Getenv("DSO_FORCE_MODE"); envMode != "" {
		return strings.ToLower(envMode), "env"
	}

	hasCloudEtc := false
	if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
		hasCloudEtc = true
	}
	hasCloudLocal := false
	if _, err := os.Stat("dso.yaml"); err == nil {
		hasCloudLocal = true
	}
	hasExplicitConfig := false
	if configPath != "" && configPath != "dso.yaml" {
		if _, err := os.Stat(configPath); err == nil {
			hasExplicitConfig = true
		}
	}

	hasLocalVault := false
	home, _ := os.UserHomeDir()
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".dso", "vault.enc")); err == nil {
			hasLocalVault = true
		}
	}

	// Conflict Check
	if (hasCloudEtc || hasCloudLocal || hasExplicitConfig) && hasLocalVault {
		fmt.Println("[DSO] ⚠️ Both local vault and cloud configuration detected. Defaulting to CLOUD mode.")
		if hasExplicitConfig {
			return "cloud", fmt.Sprintf("explicit config (%s)", configPath)
		}
		if hasCloudEtc {
			return "cloud", "auto-detected (/etc/dso/dso.yaml)"
		}
		return "cloud", "auto-detected (./dso.yaml)"
	}

	if hasExplicitConfig {
		return "cloud", fmt.Sprintf("explicit config (%s)", configPath)
	}
	if hasCloudEtc {
		return "cloud", "auto-detected (/etc/dso/dso.yaml)"
	}
	if hasCloudLocal {
		return "cloud", "auto-detected (./dso.yaml)"
	}
	if hasLocalVault {
		return "local", "auto-detected (~/.dso/vault.enc)"
	}

	// Default to cloud
	return "cloud", "default fallback"
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
		Long:               "The 'up' command is the primary entrypoint for DSO. It performs a Docker Compose deployment with secure secret injection.\n\nDSO defaults to Cloud Mode (requires systemd/root configuration). Local Mode is available as an optional developer add-on by running 'docker dso init' or passing '--mode=local'.",
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

			mode, reason := detectMode(flagMode, configPath)

			if mode == "cloud" {
				if reason == "default fallback" {
					fmt.Fprintln(os.Stderr, "No configuration found.\n\nChoose a mode:\n- Local: docker dso init\n- Cloud: sudo docker dso system setup")
					os.Exit(1)
				}

				fmt.Printf("[DSO] Running in CLOUD mode (%s)\n", reason)
				fmt.Printf("[DSO] Using provider config: %s\n", configPath)
				fmt.Println("[DSO] ⚠️  Secrets will be fetched from external providers")

				content, err := os.ReadFile(composeFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading compose file: %v\n", err)
					os.Exit(1)
				}

				if strings.Contains(string(content), "dsofile://") {
					fmt.Fprintln(os.Stderr, "Error: dsofile:// protocol is only supported in LOCAL mode. Please migrate to the Native Vault.")
					os.Exit(1)
				}

				checkCloudAgent()
				err = core.RunComposeUpWithEnv(composeFile, dockerArgs, configPath, dryRun)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error running up: %v\n", err)
					os.Exit(1)
				}
			} else {
				fmt.Printf("[DSO] Running in LOCAL mode (%s)\n", reason)
				fmt.Println("[DSO] Resolving secrets...")

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
