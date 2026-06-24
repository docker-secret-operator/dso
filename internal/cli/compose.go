package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/docker-secret-operator/dso/internal/core"
	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/spf13/cobra"
)

func extractConfigFromArgs(osArgs []string) string {
	for i, arg := range osArgs {
		if arg == "--config" || arg == "-c" {
			if i+1 < len(osArgs) {
				return osArgs[i+1]
			}
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return ""
}

func splitEnv(e string) (string, string) {
	for i := 0; i < len(e); i++ {
		if e[i] == '=' {
			return e[:i], e[i+1:]
		}
	}
	return e, ""
}

func validateDockerArgs(args []string) error {
	allowedCmds := map[string]bool{
		"up": true, "down": true, "ps": true, "logs": true,
		"stop": true, "restart": true, "pull": true,
	}

	foundCmd := false
	for _, arg := range args {
		// Reject shell metacharacters (G204)
		if strings.ContainsAny(arg, ";&|$`\"") {
			return fmt.Errorf("invalid character in arguments: %s", arg)
		}

		// The first non-flag argument should be an allowed subcommand
		if !foundCmd && !strings.HasPrefix(arg, "-") {
			if !allowedCmds[arg] {
				return fmt.Errorf("unsupported docker compose command: %s", arg)
			}
			foundCmd = true
		}
	}
	return nil
}

// extractUpArgs detects whether dockerArgs contains an "up" subcommand and, if so,
// returns the compose file (from -f/--file), the args that follow "up", and true.
// The -f flag is stripped from the returned extraArgs since RunComposeUpWithEnv
// receives the compose file as a separate parameter.
func extractUpArgs(dockerArgs []string) (composeFile string, extraArgs []string, isUp bool) {
	upIdx := -1
	for i := 0; i < len(dockerArgs); i++ {
		arg := dockerArgs[i]
		if (arg == "-f" || arg == "--file") && i+1 < len(dockerArgs) {
			composeFile = dockerArgs[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--file=") {
			composeFile = strings.TrimPrefix(arg, "--file=")
			continue
		}
		if !strings.HasPrefix(arg, "-") && arg == "up" {
			upIdx = i
			break
		}
	}
	if upIdx == -1 {
		return "", nil, false
	}
	// Collect args after "up", stripping any -f flags that appear there too
	for i := upIdx + 1; i < len(dockerArgs); i++ {
		arg := dockerArgs[i]
		if (arg == "-f" || arg == "--file") && i+1 < len(dockerArgs) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--file=") {
			continue
		}
		extraArgs = append(extraArgs, arg)
	}
	return composeFile, extraArgs, true
}

func NewComposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "compose [args...]",
		Short:              "Wrapper around docker compose that injects secrets",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			configPath := extractConfigFromArgs(os.Args)
			if configPath == "" {
				configPath = ResolveConfig()
			}

			// Validate arguments before any processing
			if err := validateDockerArgs(args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var dockerArgs []string
			skip := false
			for _, arg := range args {
				if skip {
					skip = false
					continue
				}
				if arg == "--config" || arg == "-c" {
					skip = true
					continue
				}
				if strings.HasPrefix(arg, "--config=") {
					continue
				}
				dockerArgs = append(dockerArgs, arg)
			}

			// For 'up', delegate to RunComposeUpWithEnv which injects DSO labels
			// (dso.reloader, dso.secrets, dso.compose.path) into every service so
			// the rotation agent can track containers without manual config.
			composeFile, upExtraArgs, isUp := extractUpArgs(dockerArgs)
			if isUp {
				if composeFile == "" {
					if _, err := os.Stat("docker-compose.yml"); err == nil {
						composeFile = "docker-compose.yml"
					} else if _, err := os.Stat("docker-compose.yaml"); err == nil {
						composeFile = "docker-compose.yaml"
					} else {
						fmt.Fprintln(os.Stderr, "Error: No docker-compose.yml or docker-compose.yaml found.")
						os.Exit(1)
					}
				}
				if err := core.RunComposeUpWithEnv(composeFile, upExtraArgs, configPath, false); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			}

			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\nTip: Create /etc/dso/dso.yaml or pass --config /path/to/dso.yaml\n", err)
				os.Exit(1)
			}

			socketPath := DefaultSocketPath()
			if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
				socketPath = custom
			}

			client, err := injector.NewAgentClient(socketPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Agent connection failed. Is the DSO agent running? Error: %v\n", err)
				os.Exit(1)
			}
			defer client.Close()

			injectedEnvs, err := client.FetchAllEnvs(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching secrets: %v\n", err)
				os.Exit(1)
			}

			envMap := make(map[string]string)
			for _, e := range os.Environ() {
				k, v := splitEnv(e)
				envMap[k] = v
			}
			for k, v := range injectedEnvs {
				envMap[k] = v
			}

			var finalEnvs []string
			for k, v := range envMap {
				finalEnvs = append(finalEnvs, fmt.Sprintf("%s=%s", k, v))
			}

			dockerPath, err := exec.LookPath("docker")
			if err != nil {
				fmt.Fprintln(os.Stderr, "docker executable not found in PATH")
				os.Exit(1)
			}

			fullArgs := append([]string{"docker", "compose"}, dockerArgs...)
			// #nosec G204 -- docker execution uses strictly validated arguments
			if err := syscall.Exec(dockerPath, fullArgs, finalEnvs); err != nil {
				fmt.Fprintf(os.Stderr, "Exec failed: %v\n", err)
				os.Exit(1)
			}
		},
	}
}
