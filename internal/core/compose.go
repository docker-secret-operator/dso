package core

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker-secret-operator/dso/internal/injector"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"gopkg.in/yaml.v3"
)

var debugMode bool

func SetDebug(b bool) {
	debugMode = b
}

type ComposeFile struct {
	Version  string                 `yaml:"version,omitempty"`
	Services map[string]interface{} `yaml:"services,omitempty"`
	Secrets  map[string]interface{} `yaml:"secrets,omitempty"`
	Other    map[string]interface{} `yaml:",inline"`
}

// RunComposeUpWithEnv parses the compose file, fetches DSO custom secrets for env overrides, merges them with dso.yaml configurations, and dynamically runs docker compose up via stdin.
func RunComposeUpWithEnv(filename string, extraArgs []string, configPath string, dryRun bool) error {
	envMap := make(map[string]string)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Resolve config path if empty
	targetConfig := configPath
	if targetConfig == "" {
		if _, err := os.Stat("dso.yaml"); err == nil {
			targetConfig = "dso.yaml"
		} else if _, err := os.Stat("/etc/dso/dso.yaml"); err == nil {
			targetConfig = "/etc/dso/dso.yaml"
		}
	}

	var injectedSecrets map[string]string
	cfg, err := config.LoadConfig(targetConfig)
	if err == nil {
		socketPath := "/var/run/dso.sock"
		if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
			socketPath = custom
		}
		client, err := injector.NewAgentClient(socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Agent connection failed (%v). Proceeding without dynamic env injection.\n", err)
		} else {
			injectedSecrets, err = client.FetchAllEnvs(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: Injection failed: %v\n", err)
				os.Exit(1)
			}
			for k, v := range injectedSecrets {
				envMap[k] = v
			}
		}
	}

	// G304: Ensure the filename is safe and does not escape the workspace
	safePath, err := config.IsSafePath("", filename)
	if err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	data, err := os.ReadFile(safePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file %s: %w", filename, err)
	}

	var parsed ComposeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	// Step 1: Inject rotation management labels and secrets into all services
	absPath, _ := filepath.Abs(filename)
	for name, svcRaw := range parsed.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		// 1.1 Inject Labels
		labels := make(map[string]interface{})
		if existingLabels, ok := svc["labels"].(map[string]interface{}); ok {
			labels = existingLabels
		} else if existingLabels, ok := svc["labels"].([]interface{}); ok {
			for _, l := range existingLabels {
				parts := strings.SplitN(fmt.Sprintf("%v", l), "=", 2)
				if len(parts) == 2 {
					labels[parts[0]] = parts[1]
				} else {
					labels[parts[0]] = ""
				}
			}
		}

		labels["dso.reloader"] = "true"
		labels["dso.compose.path"] = absPath

		var used []string
		if cfg != nil {
			for _, s := range cfg.Secrets {
				used = append(used, s.Name)
			}
		}
		if len(used) > 0 {
			labels["dso.secrets"] = strings.Join(used, ",")
		}
		svc["labels"] = labels

		// 1.2 Inject Secrets based on Mode (env/file)
		envSection := make(map[string]interface{})
		if existingEnv, ok := svc["environment"].(map[string]interface{}); ok {
			envSection = existingEnv
		} else if existingEnv, ok := svc["environment"].([]interface{}); ok {
			for _, e := range existingEnv {
				parts := strings.SplitN(fmt.Sprintf("%v", e), "=", 2)
				if len(parts) == 2 {
					envSection[parts[0]] = parts[1]
				}
			}
		}

		var tmpfsMounts []interface{}
		if existingTmpfs, ok := svc["tmpfs"].([]interface{}); ok {
			tmpfsMounts = existingTmpfs
		} else if existingTmpfs, ok := svc["tmpfs"].(string); ok {
			tmpfsMounts = append(tmpfsMounts, existingTmpfs)
		}

		if cfg != nil {
			for _, sec := range cfg.Secrets {
				if sec.Inject == "file" && sec.Path != "" {
					// For file mode, we ONLY mount tmpfs. 
					// Data is injected LATER via direct tar streaming by the agent/watcher.
					tmpfsMounts = append(tmpfsMounts, sec.Path)
					continue
				}

				// Standard ENV mode
				for _, envName := range sec.Mappings {
					if val, ok := injectedSecrets[envName]; ok {
						envSection[envName] = val
					}
				}
			}
		}

		if len(tmpfsMounts) > 0 {
			svc["tmpfs"] = tmpfsMounts
		}
		svc["environment"] = envSection
		parsed.Services[name] = svc
	}

	// Always use the transformed YAML to ensure labels and secrets are injected
	transformedData, err := yaml.Marshal(&parsed)
	if err != nil {
		return fmt.Errorf("failed to marshal transformed compose file: %w", err)
	}

	// Step 2: Run docker compose via stdin
	projectName := filepath.Base(filepath.Dir(absPath))
	
	// We use '-f -' to read the transformed YAML from stdin, avoiding disk leakage.
	args := append([]string{"compose", "-p", projectName, "-f", "-", "up"}, extraArgs...)
	cmd := exec.Command("docker", args...) 
	
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(string(transformedData)) 

	// Rebuild process environment for the docker command
	var finalEnvs []string
	for k, v := range envMap {
		finalEnvs = append(finalEnvs, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = finalEnvs

	if dryRun {
		fmt.Printf("DRY RUN: DSO would securely inject the following secrets into %s (in-memory transformation):\n", filename)
		for k := range injectedSecrets {
			fmt.Printf("  - [Service Environment] %s=********\n", k)
		}
		fmt.Println("DRY RUN completed successfully. Use without --dry-run to deploy.")
		return nil
	}

	if debugMode {
		fmt.Println("DEBUG: Transformed Compose File (Redacted):")
		PrintRedactedCompose(&parsed)
	}

	fmt.Printf("DSO securely injecting secrets for %s (via in-memory pipe)...\n", filename)
	return cmd.Run()
}

func PrintRedactedCompose(p *ComposeFile) {
	// Deep copy and redact
	redacted := *p
	redacted.Services = make(map[string]interface{})
	for name, svcRaw := range p.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			redacted.Services[name] = svcRaw
			continue
		}
		
		newSvc := make(map[string]interface{})
		for k, v := range svc {
			if k == "environment" {
				env, ok := v.(map[string]interface{})
				if ok {
					redactedEnv := make(map[string]interface{})
					for ek := range env {
						redactedEnv[ek] = observability.Redact(fmt.Sprintf("%v", env[ek]))
					}
					newSvc[k] = redactedEnv
				} else {
					newSvc[k] = v
				}
			} else {
				newSvc[k] = v
			}
		}
		redacted.Services[name] = newSvc
	}
	
	d, _ := yaml.Marshal(&redacted)
	fmt.Println(string(d))
}


func fetchSecretDirectly(provider, secretPath string) (string, error) {
	socketPath := "/var/run/dso.sock"
	if custom := os.Getenv("DSO_SOCKET_PATH"); custom != "" {
		socketPath = custom
	}

	client, err := injector.NewAgentClient(socketPath)
	if err != nil {
		return "", fmt.Errorf("agent connection failed: %w", err)
	}

	data, err := client.FetchSecret(provider, map[string]string{}, secretPath)
	if err != nil {
		return "", err
	}

	if len(data) == 1 {
		for _, v := range data {
			return v, nil
		}
	}

	bytes, _ := json.Marshal(data)
	return string(bytes), nil
}
