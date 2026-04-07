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
// If preInjected is non-nil, those secrets are used directly instead of connecting back to the agent (used during rotation to avoid self-call deadlock).
func RunComposeUpWithEnv(filename string, extraArgs []string, configPath string, dryRun bool, preInjected ...map[string]string) error {
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

	// Always load config (needed for label injection below, regardless of how secrets are fetched)
	cfg, _ := config.LoadConfig(targetConfig)

	var injectedSecrets map[string]string

	// If the caller pre-injected secrets (e.g. during rotation), use those directly.
	// This avoids a self-call deadlock where the agent tries to talk to itself.
	if len(preInjected) > 0 && preInjected[0] != nil {
		injectedSecrets = preInjected[0]
		for k, v := range injectedSecrets {
			envMap[k] = v
		}
	} else if cfg != nil {
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

	// Step 1: Inject rotation management labels into each service
	absPath, _ := filepath.Abs(filename)
	for name, svcRaw := range parsed.Services {
		svc, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		var tmpfsMounts []string

		// Step 1: Filter services by Targets (STRICT mode)
		if cfg != nil {
			for _, sec := range cfg.Secrets {
				// Check if this service is a target for this secret
				isTarget := false
				
				// 1.1 Explicit container naming
				if len(sec.Targets.Containers) > 0 {
					for _, targetName := range sec.Targets.Containers {
						if targetName == name {
							isTarget = true
							break
						}
					}
				}
				
				// 1.2 Label-based matching (Exact match V1)
				if !isTarget && len(sec.Targets.Labels) > 0 {
					svcLabels := make(map[string]string)
					if labelsRaw, ok := svc["labels"].(map[string]interface{}); ok {
						for k, v := range labelsRaw {
							svcLabels[k] = fmt.Sprintf("%v", v)
						}
					}
					
					matchesAll := true
					for k, v := range sec.Targets.Labels {
						if svcLabels[k] != v {
							matchesAll = false
							break
						}
					}
					if matchesAll {
						isTarget = true
					}
				}

				// 1.3 Fallback to Legacy Discovery (if no targets defined)
				if len(sec.Targets.Containers) == 0 && len(sec.Targets.Labels) == 0 {
					// In legacy mode, we check if the service has 'dso.reloader=true'
					// and manually check if it was intended. 
					// For 'docker dso up', we usually inject into ALL unless specified.
					isTarget = true 
				}

				if !isTarget {
					continue
				}

				// 2. Inject Labels for the Agent to discover
				labels := make(map[string]interface{})
				if existingLabels, ok := svc["labels"].(map[string]interface{}); ok {
					labels = existingLabels
				}
				labels["dso.reloader"] = "true"
				labels["dso.compose.path"] = absPath
				
				// Track secrets for this service
				existingSecrets := ""
				if s, ok := labels["dso.secrets"].(string); ok {
					existingSecrets = s
				}
				if existingSecrets == "" {
					labels["dso.secrets"] = sec.Name
				} else if !strings.Contains(existingSecrets, sec.Name) {
					labels["dso.secrets"] = existingSecrets + "," + sec.Name
				}
				svc["labels"] = labels

				// 3. Inject Values based on Mode (env/file)
				injectConfig := sec.Inject
				// Apply defaults
				if injectConfig.Type == "" {
					injectConfig = cfg.Defaults.Inject
				}

				if injectConfig.Type == "file" && injectConfig.Path != "" {
					// Mount tmpfs for file mode
					tmpfsMounts = append(tmpfsMounts, injectConfig.Path)
				} else {
					// Standard ENV mode
					// Handle case where environment is a list vs map in the original docker-compose.yml
					var envSection map[string]interface{}
					if existingEnvMap, ok := svc["environment"].(map[string]interface{}); ok {
						envSection = existingEnvMap
					} else if existingEnvSlice, ok := svc["environment"].([]interface{}); ok {
						// Convert slice to map
						envSection = make(map[string]interface{})
						for _, item := range existingEnvSlice {
							strItem := fmt.Sprintf("%v", item)
							parts := strings.SplitN(strItem, "=", 2)
							if len(parts) == 2 {
								envSection[parts[0]] = parts[1]
							} else if len(parts) == 1 {
								// e.g., - MYSQL_ROOT_PASSWORD
								envSection[parts[0]] = nil
							}
						}
					} else {
						envSection = make(map[string]interface{})
					}
					
					for keyInProvider, envName := range sec.Mappings {
						if val, ok := injectedSecrets[envName]; ok {
							envSection[envName] = val
						} else if val, ok := injectedSecrets[keyInProvider]; ok {
							// Support mapping by provider key if env name is not found
							envSection[envName] = val
						}
					}
					svc["environment"] = envSection
				}
			}
		}

		if len(tmpfsMounts) > 0 {
			svc["tmpfs"] = tmpfsMounts
		}
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
