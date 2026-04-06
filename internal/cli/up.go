package cli

import (
	"fmt"
	"os"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/core"
	"github.com/spf13/cobra"
)

func ensureAgentRunning(configPath string) {
	socketPath := "/var/run/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	// 1. Probe the socket
	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err == nil {
		conn.Close()
		return // Agent is responsive
	}

	// 2. Start agent in background if not responsive
	fmt.Println("🚀 Starting DSO agent...")
	
	// We use the same binary (os.Args[0]) with the 'agent' command
	args := []string{"agent"}
	if configPath != "" && configPath != "dso.yaml" {
		args = append(args, "--config", configPath)
	}

	cmd := exec.Command(os.Args[0], args...)
	
	// Start in background, detached from terminal
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	err = cmd.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start DSO agent: %v\n", err)
		return
	}

	// Wait for socket to initialize
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

func NewUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "up [args...]",
		Short:              "Deploy a stack and automatically start the DSO agent",
		Long:               "The 'up' command is the primary entrypoint for DSO. It performs a Docker Compose deployment with secure secret injection and automatically ensures the DSO agent is running in the background. All standard Docker Compose flags (like -d, --build) are supported and forwarded directly.",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			composeFile := ""
			configPath := extractConfigFromArgs(os.Args)
			if configPath == "" {
				configPath = ResolveConfig()
			}
			ensureAgentRunning(configPath)

			var dockerArgs []string
			skip := false
			dryRun := false
			for i, arg := range args {
				if skip {
					skip = false
					continue
				}
				if arg == "-f" || arg == "--file" || arg == "-cf" {
					if i+1 < len(args) {
						composeFile = args[i+1]
					}
					skip = true
					continue
				}
				if strings.HasPrefix(arg, "--file=") || strings.HasPrefix(arg, "-cf=") {
					composeFile = strings.TrimPrefix(strings.TrimPrefix(arg, "--file="), "-cf=")
					continue
				}
				if arg == "--config" || arg == "-c" {
					skip = true
					continue
				}
				if strings.HasPrefix(arg, "--config=") {
					continue
				}
				if arg == "--dry-run" {
					dryRun = true
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

			// Core compose logic: parse, fetch secrets, rewrite and execute
			err := core.RunComposeUpWithEnv(composeFile, dockerArgs, configPath, dryRun)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error running up: %v\n", err)
				os.Exit(1)
			}
		},
	}
}
