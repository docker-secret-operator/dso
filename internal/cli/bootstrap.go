package cli

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/pkg/vault"
	"github.com/spf13/cobra"
)

// NewBootstrapCmd creates the bootstrap command with subcommands for local and agent modes
func NewBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap [local|agent]",
		Short: "Initialize DSO runtime environment",
		Long: `Initialize DSO for either local development or production agent mode.

Bootstrap creates the runtime directory structure, generates configuration,
initializes encryption, and validates your environment.

Examples:
  docker dso bootstrap local              # For local development
  sudo docker dso bootstrap agent         # For production deployment`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := args[0]

			switch mode {
			case "local":
				return bootstrapLocal()
			case "agent":
				return bootstrapAgent()
			default:
				return fmt.Errorf("invalid mode: %s (expected 'local' or 'agent')", mode)
			}
		},
	}

	return cmd
}

// ════════════════════════════════════════════════════════════════════════════
// LOCAL MODE BOOTSTRAP
// ════════════════════════════════════════════════════════════════════════════

func bootstrapLocal() error {
	fmt.Println()
	fmt.Println("Initializing DSO Local Runtime...")
	fmt.Println()

	// Step 1: Privilege check (must NOT be root)
	if os.Geteuid() == 0 {
		return fmt.Errorf(
			"'dso bootstrap local' must NOT be run as root.\n" +
				"  The local vault must be owned by your user account.\n" +
				"  Please re-run without sudo: dso bootstrap local",
		)
	}

	// Step 2: Validate Docker
	fmt.Print("  Validating Docker... ")
	if err := validateDockerConnectivity(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("Docker validation failed: %w", err)
	}
	fmt.Println("✓")

	// Step 3: Create directory structure
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	dsoDir := filepath.Join(homeDir, ".dso")
	fmt.Print("  Creating directories... ")
	if err := createLocalDirectoryStructure(dsoDir); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓")

	// Step 4: Check for existing vault
	vaultPath := filepath.Join(dsoDir, "vault.enc")
	if _, err := os.Stat(vaultPath); err == nil {
		return fmt.Errorf(
			"vault already exists at %s\n"+
				"  To reset, remove: rm -rf %s\n"+
				"  Then run: dso bootstrap local",
			vaultPath, dsoDir,
		)
	}

	// Step 5: Initialize vault
	fmt.Print("  Initializing encryption vault... ")
	if err := vault.InitDefault(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("failed to initialize vault: %w", err)
	}
	fmt.Println("✓")

	// Step 6: Generate configuration
	configPath := filepath.Join(dsoDir, "config.yaml")
	fmt.Print("  Generating configuration... ")
	if err := generateLocalConfig(configPath); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓")

	// Step 7: Print success message
	fmt.Println()
	printBootstrapSuccessLocal(dsoDir)

	return nil
}

func createLocalDirectoryStructure(baseDir string) error {
	dirs := []struct {
		path string
		perm os.FileMode
	}{
		{baseDir, 0700},
		{filepath.Join(baseDir, "vault"), 0700},
		{filepath.Join(baseDir, "state"), 0700},
		{filepath.Join(baseDir, "cache"), 0700},
		{filepath.Join(baseDir, "logs"), 0700},
		{filepath.Join(baseDir, "plugins"), 0700},
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.perm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d.path, err)
		}
	}

	return nil
}

func generateLocalConfig(path string) error {
	configContent := `version: v1alpha1

runtime:
  mode: local
  log_level: info

providers:
  local:
    type: file
    enabled: true
    path: ~/.dso/vault

agent:
  cache:
    ttl: 1h
    max_size: 100Mi

  watch:
    polling_interval: 5m
    debounce_window: 5s

  health_check:
    timeout: 30s
    retries: 3

  rotation:
    strategy: restart
    timeout: 30s
    rollback_on_failure: true
`

	return os.WriteFile(path, []byte(configContent), 0600)
}

func printBootstrapSuccessLocal(dsoDir string) {
	fmt.Println("┌────────────────────────────────────┐")
	fmt.Println("│ DSO Local Runtime Initialized      │")
	fmt.Println("├────────────────────────────────────┤")
	fmt.Println("│ Mode: development                  │")
	fmt.Println("│ Provider: local encrypted vault    │")
	fmt.Println("│ Docker: connected ✓                │")
	fmt.Println("│ Vault: initialized ✓               │")
	fmt.Printf("│ Config: %s│\n", padRight(filepath.Join(dsoDir, "config.yaml"), 25))
	fmt.Println("└────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  docker dso secret set app/db_password")
	fmt.Println("  docker dso compose up")
	fmt.Println()
	fmt.Println("Diagnostics:")
	fmt.Println("  docker dso doctor          # validate setup")
	fmt.Println("  docker dso status          # runtime status")
	fmt.Println("  docker dso config show     # view configuration")
	fmt.Println()
}

// ════════════════════════════════════════════════════════════════════════════
// AGENT MODE BOOTSTRAP
// ════════════════════════════════════════════════════════════════════════════

func bootstrapAgent() error {
	fmt.Println()
	fmt.Println("Initializing DSO Agent Runtime...")
	fmt.Println()

	// Step 1: Privilege check (must be root)
	if os.Geteuid() != 0 {
		return fmt.Errorf(
			"'dso bootstrap agent' requires root privileges.\n" +
				"  Please re-run with sudo: sudo dso bootstrap agent",
		)
	}

	// Step 2: Validate Linux/systemd
	fmt.Print("  Validating systemd... ")
	if err := validateSystemd(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("systemd validation failed: %w", err)
	}
	fmt.Println("✓")

	// Step 3: Validate Docker
	fmt.Print("  Validating Docker... ")
	if err := validateDockerConnectivity(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("Docker validation failed: %w", err)
	}
	fmt.Println("✓")

	// Step 4: Create directory structure
	fmt.Print("  Creating runtime directories... ")
	if err := createAgentDirectoryStructure(); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓")

	// Step 4.5: Detect cloud provider
	fmt.Print("  Detecting cloud provider... ")
	provider := detectCloudProvider()
	if provider != "" {
		fmt.Printf("✓ (%s)\n", provider)
	} else {
		fmt.Println("⊘ (generic/on-premise)")
	}

	// Step 4.6: Collect secrets from user
	fmt.Println()
	secrets := collectSecretsFromUser(provider)

	// Step 5: Generate configuration
	fmt.Print("  Generating configuration... ")
	if err := generateAgentConfigWithSecrets("/etc/dso/dso.yaml", provider, secrets); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓")

	// Step 6: Create systemd service (optional - may warn if already exists)
	fmt.Print("  Setting up systemd service... ")
	if err := createSystemdServiceFile(); err != nil {
		fmt.Println("⚠")
		fmt.Printf("    Warning: %v (non-fatal)\n", err)
	} else {
		fmt.Println("✓")
	}

	// Step 7: Verify permissions
	fmt.Print("  Verifying permissions... ")
	if err := verifyAgentPermissions(); err != nil {
		fmt.Println("✗")
		return err
	}
	fmt.Println("✓")

	// Step 8: Print success message
	fmt.Println()
	printBootstrapSuccessAgent()

	return nil
}

func createAgentDirectoryStructure() error {
	dirs := []struct {
		path string
		perm os.FileMode
	}{
		{"/etc/dso", 0750},
		{"/var/lib/dso", 0750},
		{"/var/lib/dso/state", 0750},
		{"/var/lib/dso/cache", 0750},
		{"/var/lib/dso/locks", 0750},
		{"/var/lib/dso/plugins", 0750},
		{"/var/log/dso", 0750},
		{"/run/dso", 0755},
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.perm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", d.path, err)
		}
	}

	return nil
}

// detectCloudProvider automatically detects the cloud provider
// Returns one of: "aws", "azure", "huawei", "gcp" or empty string if none detected
func detectCloudProvider() string {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// Check AWS (EC2 metadata service)
	if isAWS(client) {
		return "aws"
	}

	// Check Azure
	if isAzure(client) {
		return "azure"
	}

	// Check Huawei Cloud
	if isHuawei(client) {
		return "huawei"
	}

	// Check GCP
	if isGCP(client) {
		return "gcp"
	}

	return ""
}

func isAWS(client *http.Client) bool {
	req, err := http.NewRequest("GET", "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "dso-cloud-detector")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	return false
}

func isAzure(client *http.Client) bool {
	req, err := http.NewRequest("GET", "http://169.254.169.254/metadata/instance?api-version=2021-02-01", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Metadata", "true")
	req.Header.Set("User-Agent", "dso-cloud-detector")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	return false
}

func isHuawei(client *http.Client) bool {
	req, err := http.NewRequest("GET", "http://169.254.169.254/openstack/latest/meta_data.json", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "dso-cloud-detector")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	return false
}

func isGCP(client *http.Client) bool {
	req, err := http.NewRequest("GET", "http://metadata.google.internal/computeMetadata/v1/", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Metadata-Flavor", "Google")
	req.Header.Set("User-Agent", "dso-cloud-detector")
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	return false
}

// SecretConfig holds user-provided secret information
type SecretConfig struct {
	Name       string // ARN, Key Vault name, etc.
	Mappings   map[string]string
	Provider   string
}

// collectSecretsFromUser interactively collects secret information from the user
func collectSecretsFromUser(provider string) []SecretConfig {
	reader := bufio.NewReader(os.Stdin)
	var secrets []SecretConfig

	fmt.Println("┌─────────────────────────────────────┐")
	fmt.Println("│     Configure Secret Provider       │")
	fmt.Println("└─────────────────────────────────────┘")
	fmt.Println()

	// Show provider-specific examples
	showProviderExamples(provider)
	fmt.Println()

	for {
		fmt.Print("Enter secret name/ARN (or 'done' to finish): ")
		secretName, _ := reader.ReadString('\n')
		secretName = strings.TrimSpace(secretName)

		if strings.ToLower(secretName) == "done" {
			break
		}

		if secretName == "" {
			fmt.Println("  ⚠ Secret name cannot be empty. Please try again.")
			continue
		}

		// Ask for mappings
		fmt.Println()
		fmt.Println("  Configure key mappings (e.g., MYSQL_PASSWORD=db_password)")
		fmt.Print("  Enter mappings (comma-separated, or press Enter to skip): ")
		mappingsInput, _ := reader.ReadString('\n')
		mappingsInput = strings.TrimSpace(mappingsInput)

		mappings := parseMappings(mappingsInput)

		secrets = append(secrets, SecretConfig{
			Name:     secretName,
			Mappings: mappings,
			Provider: provider,
		})

		fmt.Printf("  ✓ Added secret: %s\n\n", secretName)
	}

	return secrets
}

// showProviderExamples displays provider-specific examples
func showProviderExamples(provider string) {
	switch provider {
	case "aws":
		fmt.Println("  📌 AWS Examples:")
		fmt.Println("     ARN format: arn:aws:secretsmanager:REGION:ACCOUNT:secret:SECRET_NAME")
		fmt.Println("     Example: arn:aws:secretsmanager:us-east-1:123456789:secret:my-db-creds")
		fmt.Println()

	case "azure":
		fmt.Println("  📌 Azure Examples:")
		fmt.Println("     Key Vault URL format: https://VAULT_NAME.vault.azure.net/secrets/SECRET_NAME")
		fmt.Println("     Example: https://my-vault.vault.azure.net/secrets/db-password")
		fmt.Println()

	case "huawei":
		fmt.Println("  📌 Huawei Cloud Examples:")
		fmt.Println("     Secret name format: REGION/PROJECT_NAME/SECRET_NAME")
		fmt.Println("     Example: cn-east-2/my-project/db-credentials")
		fmt.Println()

	case "gcp":
		fmt.Println("  📌 GCP Examples:")
		fmt.Println("     Secret name format: projects/PROJECT_ID/secrets/SECRET_NAME/versions/latest")
		fmt.Println("     Example: projects/my-gcp-project/secrets/db-password/versions/latest")
		fmt.Println()

	default:
		fmt.Println("  📌 No cloud provider detected (on-premise/generic)")
		fmt.Println("     Configure your secret provider manually in /etc/dso/dso.yaml")
		fmt.Println()
	}
}

// parseMappings parses mapping string (e.g., "PASSWORD=db_password,USER=db_user")
func parseMappings(mappingsInput string) map[string]string {
	mappings := make(map[string]string)

	if mappingsInput == "" {
		return mappings
	}

	for _, pair := range strings.Split(mappingsInput, ",") {
		parts := strings.Split(strings.TrimSpace(pair), "=")
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			mappings[key] = value
		}
	}

	return mappings
}

// generateAgentConfigWithSecrets generates config with provider and user secrets
func generateAgentConfigWithSecrets(path string, provider string, secrets []SecretConfig) error {
	configContent := generateConfigContentWithSecrets(provider, secrets)
	return os.WriteFile(path, []byte(configContent), 0600)
}

// generateConfigContentWithSecrets generates YAML with secrets
func generateConfigContentWithSecrets(provider string, secrets []SecretConfig) string {
	baseConfig := `version: v1alpha1

runtime:
  mode: agent
  log_level: info

providers:
  vault:
    enabled: false
    # addr: https://vault.example.com:8200
    # auth:
    #   method: token
    #   token_env: VAULT_TOKEN

  aws:
    enabled: %s
    # region: us-east-1
    # credentials_source: iam_role

  azure:
    enabled: %s
    # vault_url: https://my-vault.vault.azure.net
    # tenant_id: ${AZURE_TENANT_ID}

  huawei:
    enabled: %s
    # region: cn-east-2

  gcp:
    enabled: %s
    # project_id: my-gcp-project

agent:
  cache:
    ttl: 1h
    max_size: 500Mi

  watch:
    polling_interval: 5m
    debounce_window: 5s

  health_check:
    timeout: 30s
    retries: 3

  rotation:
    strategy: restart
    timeout: 30s
    rollback_on_failure: true

secrets:%s
`

	// Set provider flags
	awsEnabled := "false"
	azureEnabled := "false"
	huaweiEnabled := "false"
	gcpEnabled := "false"

	if provider != "" {
		switch provider {
		case "aws":
			awsEnabled = "true"
		case "azure":
			azureEnabled = "true"
		case "huawei":
			huaweiEnabled = "true"
		case "gcp":
			gcpEnabled = "true"
		}
	}

	// Build secrets section
	secretsYAML := ""
	if len(secrets) > 0 {
		secretsYAML = "\n"
		for _, secret := range secrets {
			secretsYAML += fmt.Sprintf("  - name: %s\n", secret.Name)
			secretsYAML += fmt.Sprintf("    provider: %s\n", provider)
			secretsYAML += "    rotation:\n"
			secretsYAML += "      strategy: restart\n"
			secretsYAML += "    mappings:\n"

			if len(secret.Mappings) > 0 {
				for key, value := range secret.Mappings {
					secretsYAML += fmt.Sprintf("      %s: %s\n", key, value)
				}
			} else {
				secretsYAML += "      # Add your key mappings here\n"
			}
		}
	} else {
		secretsYAML = "\n  # Add your secrets here\n"
	}

	return fmt.Sprintf(baseConfig, awsEnabled, azureEnabled, huaweiEnabled, gcpEnabled, secretsYAML)
}

// generateAgentConfigWithProvider generates config with auto-detected provider enabled
func generateAgentConfigWithProvider(path string, provider string) error {
	configContent := generateConfigContent(provider)
	return os.WriteFile(path, []byte(configContent), 0600)
}

// generateConfigContent generates the YAML config with the appropriate provider enabled
func generateConfigContent(provider string) string {
	baseConfig := `version: v1alpha1

runtime:
  mode: agent
  log_level: info

providers:
  vault:
    enabled: false
    # addr: https://vault.example.com:8200
    # auth:
    #   method: token
    #   token_env: VAULT_TOKEN

  aws:
    enabled: %s
    # region: us-east-1
    # credentials_source: iam_role  # or env_vars, shared_credentials_file

  azure:
    enabled: %s
    # vault_url: https://my-vault.vault.azure.net
    # tenant_id: ${AZURE_TENANT_ID}
    # client_id: ${AZURE_CLIENT_ID}
    # client_secret: ${AZURE_CLIENT_SECRET}

  huawei:
    enabled: %s
    # region: cn-east-2
    # access_key: ${HUAWEI_ACCESS_KEY}
    # secret_key: ${HUAWEI_SECRET_KEY}

  gcp:
    enabled: %s
    # project_id: my-gcp-project
    # credentials_path: ${GOOGLE_APPLICATION_CREDENTIALS}

agent:
  cache:
    ttl: 1h
    max_size: 500Mi

  watch:
    polling_interval: 5m
    debounce_window: 5s

  health_check:
    timeout: 30s
    retries: 3

  rotation:
    strategy: restart
    timeout: 30s
    rollback_on_failure: true
`

	// Set the appropriate provider to enabled based on detection
	awsEnabled := "false"
	azureEnabled := "false"
	huaweiEnabled := "false"
	gcpEnabled := "false"

	switch provider {
	case "aws":
		awsEnabled = "true"
	case "azure":
		azureEnabled = "true"
	case "huawei":
		huaweiEnabled = "true"
	case "gcp":
		gcpEnabled = "true"
	}

	return fmt.Sprintf(baseConfig, awsEnabled, azureEnabled, huaweiEnabled, gcpEnabled)
}

func generateAgentConfig(path string) error {
	configContent := `version: v1alpha1

runtime:
  mode: agent
  log_level: info

providers:
  vault:
    enabled: false
    # addr: https://vault.example.com:8200
    # auth:
    #   method: token
    #   token_env: VAULT_TOKEN

  aws:
    enabled: false
    # region: us-east-1

  azure:
    enabled: false
    # vault_url: https://my-vault.vault.azure.net

agent:
  cache:
    ttl: 1h
    max_size: 500Mi

  watch:
    polling_interval: 5m
    debounce_window: 5s

  health_check:
    timeout: 30s
    retries: 3

  rotation:
    strategy: restart
    timeout: 30s
    rollback_on_failure: true
`

	return os.WriteFile(path, []byte(configContent), 0640)
}

func createSystemdServiceFile() error {
	serviceContent := `[Unit]
Description=DSO Secret Injection Runtime Agent
Documentation=https://github.com/docker-secret-operator/dso
After=docker.service
Requires=docker.service

[Service]
Type=simple
User=root
Group=root

WorkingDirectory=/var/lib/dso

ExecStart=/usr/local/bin/dso agent --config /etc/dso/dso.yaml

Restart=on-failure
RestartSec=10
StartLimitInterval=60s
StartLimitBurst=3

StandardOutput=journal
StandardError=journal
SyslogIdentifier=dso-agent

LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`

	serviceDir := "/etc/systemd/system"
	servicePath := filepath.Join(serviceDir, "dso-agent.service")

	// Check if service already exists
	if _, err := os.Stat(servicePath); err == nil {
		return fmt.Errorf("systemd service already exists at %s", servicePath)
	}

	return os.WriteFile(servicePath, []byte(serviceContent), 0644)
}

func verifyAgentPermissions() error {
	// Check if /etc/dso is readable/writable
	if _, err := os.Stat("/etc/dso"); err != nil {
		return fmt.Errorf("cannot access /etc/dso: %w", err)
	}

	// Check if /var/lib/dso is readable/writable
	if _, err := os.Stat("/var/lib/dso"); err != nil {
		return fmt.Errorf("cannot access /var/lib/dso: %w", err)
	}

	// Check if /var/log/dso is readable/writable
	if _, err := os.Stat("/var/log/dso"); err != nil {
		return fmt.Errorf("cannot access /var/log/dso: %w", err)
	}

	return nil
}

func printBootstrapSuccessAgent() {
	fmt.Println("┌─────────────────────────────────────┐")
	fmt.Println("│ DSO Agent Runtime Initialized       │")
	fmt.Println("├─────────────────────────────────────┤")
	fmt.Println("│ Mode: production (systemd)          │")
	fmt.Println("│ Config: /etc/dso/dso.yaml           │")
	fmt.Println("│ State: /var/lib/dso/state           │")
	fmt.Println("│ Logs: journalctl -u dso-agent       │")
	fmt.Println("│ Socket: /run/dso/agent.sock         │")
	fmt.Println("│ Permissions: verified ✓             │")
	fmt.Println("│ Docker: connected ✓                 │")
	fmt.Println("└─────────────────────────────────────┘")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit configuration:")
	fmt.Println("     sudo nano /etc/dso/dso.yaml")
	fmt.Println()
	fmt.Println("  2. Enable and start agent:")
	fmt.Println("     sudo docker dso system enable")
	fmt.Println()
	fmt.Println("  3. Verify installation:")
	fmt.Println("     docker dso doctor")
	fmt.Println("     docker dso status")
	fmt.Println()
	fmt.Println("  4. Deploy with docker-compose.yaml:")
	fmt.Println("     docker compose up")
	fmt.Println()
}

// ════════════════════════════════════════════════════════════════════════════
// VALIDATION HELPERS
// ════════════════════════════════════════════════════════════════════════════

func validateDockerConnectivity() error {
	// Try to connect to Docker socket
	socketPaths := []string{
		"/var/run/docker.sock",        // Linux
		"/var/run/docker/docker.sock", // Docker Desktop on Linux
		"/Users/docker.sock",          // Docker Desktop on Mac (common location)
	}

	for _, socketPath := range socketPaths {
		if _, err := os.Stat(socketPath); err == nil {
			return nil // Found a valid Docker socket
		}
	}

	// If we can't find any socket, warn but don't fail (could be Docker not running yet)
	return fmt.Errorf("Docker socket not found (Docker may not be running)")
}

func validateSystemd() error {
	// Check if systemd is available
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return fmt.Errorf("systemd not available (required for agent mode)")
	}

	return nil
}

// ════════════════════════════════════════════════════════════════════════════
// UTILITIES
// ════════════════════════════════════════════════════════════════════════════

func padRight(s string, length int) string {
	if len(s) >= length {
		return s + " "
	}
	padding := strings.Repeat(" ", length-len(s)+1)
	return s + padding
}
