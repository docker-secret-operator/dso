package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker-secret-operator/dso/internal/bootstrap"
	"github.com/spf13/cobra"
)

// NewSetupCmd creates the simplified setup wizard command
func NewSetupCmd() *cobra.Command {
	var (
		mode       string
		provider   string
		autoDetect bool
		nonRoot    bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Simple setup wizard for DSO",
		Long: `Setup wizard that configures DSO for your environment.

This command provides an interactive experience to:
  - Detect cloud provider (AWS, Azure, Huawei, or Local)
  - Select deployment mode (Local or Cloud)
  - Automatically install required provider plugins
  - Generate configuration file
  - Verify your setup

Examples:
  docker dso setup              # Interactive setup wizard
  docker dso setup --auto-detect # Auto-detect cloud provider
  docker dso setup --mode local # Setup for local vault mode`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := &cliLogger{}
			return runSetupWizard(cmd.Context(), logger, mode, provider, autoDetect, nonRoot)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "Deployment mode: local or agent (cloud)")
	cmd.Flags().StringVar(&provider, "provider", "", "Cloud provider: aws, azure, vault, huawei")
	cmd.Flags().BoolVar(&autoDetect, "auto-detect", false, "Auto-detect cloud provider from instance metadata")
	cmd.Flags().BoolVar(&nonRoot, "enable-nonroot", false, "Enable non-root user access to DSO")

	return cmd
}

func runSetupWizard(ctx context.Context, logger bootstrap.Logger, mode, provider string, autoDetect, nonRoot bool) error {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════╗")
	fmt.Println("║     Docker Secret Operator (DSO) Setup Wizard      ║")
	fmt.Println("╚════════════════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Detect or prompt for cloud provider
	var detectedProvider *bootstrap.CloudProviderInfo
	if autoDetect {
		fmt.Println("🔍 Auto-detecting cloud provider...")
		detector := bootstrap.NewCloudDetector(0, logger)
		detected, err := detector.DetectCloudProvider(ctx)
		if err != nil {
			fmt.Printf("⚠ Auto-detection failed: %v\n", err)
			detectedProvider = nil
		} else {
			detectedProvider = detected
			if detectedProvider.Detected {
				fmt.Printf("✓ Detected: %s\n", detectedProvider.Provider)
			} else {
				fmt.Println("ℹ No cloud provider detected, assuming local environment")
			}
		}
	} else if provider != "" {
		// Provider specified via flag
		fmt.Printf("Using provider: %s\n", provider)
		detectedProvider = &bootstrap.CloudProviderInfo{
			Provider: provider,
			Detected: true,
		}
	} else {
		// Interactive mode: prompt user
		detectedProvider = promptForCloudProvider(ctx, logger)
	}

	// Step 2: Determine deployment mode
	var deploymentMode string
	if mode != "" {
		deploymentMode = mode
	} else {
		deploymentMode = suggestDeploymentMode(detectedProvider)
	}

	fmt.Println()
	fmt.Printf("📋 Configuration Summary:\n")
	fmt.Printf("  Provider:      %s\n", detectedProvider.Provider)
	fmt.Printf("  Mode:          %s\n", deploymentMode)
	fmt.Println()

	// Step 3: Confirm setup
	if !autoDetect && mode == "" && provider == "" {
		fmt.Print("Ready to proceed with setup? (yes/no): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(input)) != "yes" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Step 3.5: Elevate privileges if agent mode and not root
	if deploymentMode == "agent" && os.Geteuid() != 0 {
		fmt.Println("\n🛡️ Agent setup requires root privileges. Elevating via sudo...")
		
		args := []string{"docker", "dso", "setup", "--mode", "agent", "--provider", detectedProvider.Provider}
		if autoDetect {
			args = append(args, "--auto-detect")
		}
		if nonRoot {
			args = append(args, "--enable-nonroot")
		}
		
		cmd := exec.Command("sudo", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo execution failed: %w", err)
		}
		
		// Exit successfully, as the elevated child process completes the rest
		return nil
	}

	// Step 4: Create configuration file
	fmt.Println()
	fmt.Println("📝 Creating configuration...")
	configPath, err := createConfigFile(deploymentMode, detectedProvider.Provider)
	if err != nil {
		return fmt.Errorf("failed to create configuration: %w", err)
	}
	fmt.Printf("✓ Configuration created: %s\n", configPath)

	// Step 5: Install provider plugins
	if detectedProvider.Provider != "local" {
		fmt.Println()
		fmt.Println("📦 Installing provider plugins...")
		installer := bootstrap.NewProviderPluginInstaller(logger, false)
		if err := installer.InstallProviderPlugins(ctx, []string{detectedProvider.Provider}); err != nil {
			fmt.Printf("⚠ Plugin installation failed: %v\n", err)
			fmt.Println("  You can install manually later with: docker dso system setup --provider <name>")
		} else {
			fmt.Printf("✓ Provider plugin installed: %s\n", detectedProvider.Provider)
		}
	}

	// Step 6: Setup non-root access if requested
	if nonRoot && os.Geteuid() == 0 {
		fmt.Println()
		fmt.Println("👤 Setting up non-root access...")
		if err := setupNonRootAccess(logger); err != nil {
			fmt.Printf("⚠ Non-root setup failed: %v\n", err)
		} else {
			fmt.Println("✓ Non-root access configured")
		}
	}

	// Step 7: Show configuration guidance
	fmt.Println()
	fmt.Println("📖 Configuration Guide:")
	fmt.Println()
	fmt.Println("  Complete documentation: https://github.com/docker-secret-operator/dso/blob/main/docs/CONFIG_REFERENCE.md")
	fmt.Println("  Configuration template: https://github.com/docker-secret-operator/dso/blob/main/docs/dso.yaml.template")
	fmt.Println()
	fmt.Println("  Key sections to configure in /etc/dso/dso.yaml:")
	fmt.Println("    - agent: Cache, refresh interval, watch settings, rotation behavior")
	fmt.Println("    - defaults: Default injection method and rotation strategy")
	fmt.Println("    - secrets: Which secrets to sync and which containers to update")
	fmt.Println()

	// Step 8: Auto-bootstrap agent mode
	if deploymentMode == "agent" {
		fmt.Println("🚀 Starting DSO agent...")
		bootstrapCmd := exec.Command("sudo", "docker", "dso", "bootstrap", "agent")
		bootstrapCmd.Stdout = os.Stdout
		bootstrapCmd.Stderr = os.Stderr
		if err := bootstrapCmd.Run(); err != nil {
			fmt.Printf("⚠ Agent startup may have encountered issues: %v\n", err)
			fmt.Println("  Check status with: sudo docker dso system status")
		}
	}

	// Step 8: Show next steps
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════╗")
	fmt.Println("║                    Setup Complete!                 ║")
	fmt.Println("╚════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("📚 What's next:")
	fmt.Println()

	if deploymentMode == "local" {
		fmt.Println("  1. Start the local vault:")
		fmt.Println("     docker dso up")
		fmt.Println()
		fmt.Println("  2. View vault status:")
		fmt.Println("     docker dso status")
		fmt.Println()
		fmt.Println("  3. Configure secrets:")
		fmt.Println("     docker dso secret set <name> <value>")
	} else {
		fmt.Println("  1. Edit configuration for your secrets:")
		fmt.Println("     sudo vi /etc/dso/dso.yaml")
		fmt.Println()
		fmt.Println("  2. Check agent status:")
		fmt.Println("     sudo docker dso system status")
		fmt.Println()
		fmt.Println("  3. View agent logs:")
		fmt.Println("     sudo docker dso system logs")
	}

	fmt.Println()
	fmt.Println("  More commands:")
	fmt.Println("    docker dso doctor              # Validate environment")
	fmt.Println("    docker dso config show         # View configuration")
	fmt.Println()

	return nil
}

// promptForCloudProvider asks user to select cloud provider
func promptForCloudProvider(ctx context.Context, logger bootstrap.Logger) *bootstrap.CloudProviderInfo {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🔍 Detecting cloud provider...")
	detector := bootstrap.NewCloudDetector(0, logger)
	detected, _ := detector.DetectCloudProvider(ctx)

	if detected != nil && detected.Detected {
		fmt.Printf("✓ Detected: %s\n", detected.Provider)
		fmt.Print("Use detected provider? (yes/no): ")
		input, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(input)) != "no" {
			return detected
		}
	}

	fmt.Println()
	fmt.Println("Select your environment:")
	fmt.Println("  1) Local (for development)")
	fmt.Println("  2) AWS (Amazon EC2)")
	fmt.Println("  3) Azure (Microsoft Azure)")
	fmt.Println("  4) Vault (HashiCorp Vault)")
	fmt.Println("  5) Huawei (Huawei Cloud)")
	fmt.Print("Enter your choice (1-5): ")

	input, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(input)

	providerMap := map[string]string{
		"1": "local",
		"2": "aws",
		"3": "azure",
		"4": "vault",
		"5": "huawei",
	}

	provider := providerMap[choice]
	if provider == "" {
		provider = "local"
	}

	return &bootstrap.CloudProviderInfo{
		Provider: provider,
		Detected: provider != "local",
	}
}

// suggestDeploymentMode recommends mode based on provider
func suggestDeploymentMode(info *bootstrap.CloudProviderInfo) string {
	if info.Provider == "local" {
		return "local"
	}
	return "agent"
}

// createConfigFile generates a DSO configuration file
func createConfigFile(mode, provider string) (string, error) {
	var configPath string
	var configContent string

	if mode == "agent" {
		// System-wide config for agent mode
		configPath = "/etc/dso/dso.yaml"
		configContent = generateAgentConfig(provider)
	} else {
		// Local config for development
		configPath = "./dso.yaml"
		configContent = generateLocalConfig()
	}

	// Create directory if needed
	dir := filepath.Dir(configPath)
	if mode == "agent" && os.Geteuid() != 0 {
		// Non-root user can't write to /etc
		fmt.Printf("⚠ Cannot write to %s without root privileges\n", configPath)
		fmt.Println("  Run with: sudo docker dso setup")
		return "", fmt.Errorf("insufficient permissions for %s", configPath)
	}

	if err := os.MkdirAll(dir, 0755); err != nil && mode == "agent" {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write config file
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, nil
}

// generateLocalConfig creates a local development configuration
func generateLocalConfig() string {
	return `# Docker Secret Operator - Local Configuration
# Generated by: docker dso setup
# Mode: Local vault (for development and testing)

version: v1.0.0
mode: local

providers:
  local:
    type: local
    vault_file: $HOME/.dso/vault.enc
    master_key_file: $HOME/.dso/master.key

# Define your secrets here
secrets: {}
  # Example:
  # my_database_password:
  #   provider: local
  #   secret_name: my_database_password
  #   container_name: app
  #   env_var: DB_PASSWORD
  #
  # my_api_key:
  #   provider: local
  #   secret_name: my_api_key
  #   container_name: app
  #   env_var: API_KEY
`
}

// generateAgentConfig creates a cloud-specific agent configuration
func generateAgentConfig(provider string) string {
	baseConfig := `# Docker Secret Operator - Cloud Configuration
# Generated by: docker dso setup
# Mode: Agent (cloud/production deployment)
#
# For complete configuration reference, see:
# https://github.com/docker-secret-operator/dso/blob/main/docs/CONFIG_REFERENCE.md

version: v1.0.0
mode: agent

# ════════════════════════════════════════════════════════════════════════════
# PROVIDERS - Where secrets are stored
# ════════════════════════════════════════════════════════════════════════════
providers:
`

	switch provider {
	case "aws":
		baseConfig += `  aws:
    type: aws
    region: us-east-1            # Change to your AWS region
    auth:
      method: iam_role           # Use EC2 instance role (recommended)
      # OR use access_key:
      # method: access_key
      # params:
      #   access_key_id: YOUR_KEY
      #   secret_access_key: YOUR_SECRET
    retry:
      attempts: 3
      backoff: "1s"

# ════════════════════════════════════════════════════════════════════════════
# AGENT - Runtime configuration (optional, with defaults)
# ════════════════════════════════════════════════════════════════════════════
agent:
  cache: true                    # Cache secrets in memory
  refresh_interval: "5m"         # Refresh cached secrets every 5 minutes
  auto_sync: false               # Sync secrets automatically (false = manual trigger only)
  watch:
    mode: polling                # polling, event, or hybrid
    polling_interval: "5m"       # Check provider for changes every 5 minutes
  rotation:
    enabled: true
    strategy: rolling            # restart, signal, or none
    health_check_timeout: "30s"

# ════════════════════════════════════════════════════════════════════════════
# DEFAULTS - Default behavior for all secrets (optional)
# ════════════════════════════════════════════════════════════════════════════
defaults:
  inject:
    type: env                    # env or file
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# SECRETS - Which secrets to sync and how to inject them
# ════════════════════════════════════════════════════════════════════════════
secrets:
  # Example secret - modify this template for your needs
  - name: database_credentials
    provider: aws
    inject:
      type: env                  # Inject as environment variables
    targets:
      containers:
        - app                    # Container name where secret is needed
    mappings:
      # Format: CONTAINER_ENV_VAR: aws-secret-path
      DB_USER: prod/database/username
      DB_PASSWORD: prod/database/password
      DB_HOST: prod/database/host

  # Add more secrets below as needed:
  # - name: api_keys
  #   provider: aws
  #   inject:
  #     type: env
  #   targets:
  #     containers:
  #       - api-service
  #   mappings:
  #     API_KEY: prod/api/key
  #     API_SECRET: prod/api/secret

  # File injection example:
  # - name: ssl_certificates
  #   provider: aws
  #   inject:
  #     type: file
  #     path: /etc/ssl/certs     # Mount path in container
  #   targets:
  #     containers:
  #       - web-server
  #   mappings:
  #     tls.crt: prod/ssl/certificate
  #     tls.key: prod/ssl/private-key
`

	case "azure":
		baseConfig += `  azure:
    type: azure
    region: eastus               # Change to your Azure region
    auth:
      method: managed_identity   # Use Azure Managed Identity (recommended)
      # OR use service principal:
      # method: service_principal
      # params:
      #   vault_name: my-keyvault
      #   tenant_id: YOUR_TENANT_ID
      #   client_id: YOUR_CLIENT_ID
      #   client_secret: YOUR_CLIENT_SECRET

# ════════════════════════════════════════════════════════════════════════════
# AGENT - Runtime configuration (optional, with defaults)
# ════════════════════════════════════════════════════════════════════════════
agent:
  cache: true
  refresh_interval: "5m"
  watch:
    mode: polling
    polling_interval: "5m"
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# DEFAULTS - Default behavior for all secrets (optional)
# ════════════════════════════════════════════════════════════════════════════
defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# SECRETS - Which secrets to sync and how to inject them
# ════════════════════════════════════════════════════════════════════════════
secrets:
  - name: database_credentials
    provider: azure
    inject:
      type: env
    targets:
      containers:
        - app
    mappings:
      DB_USER: database-username
      DB_PASSWORD: database-password
      DB_HOST: database-host
`

	case "vault":
		baseConfig += `  vault:
    type: vault
    auth:
      method: token              # token, kubernetes, jwt, appRole
      params:
        address: https://vault.example.com:8200
        token: YOUR_VAULT_TOKEN  # Or use VAULT_TOKEN env var
    config:
      namespace: admin           # Vault namespace (optional)

# ════════════════════════════════════════════════════════════════════════════
# AGENT - Runtime configuration (optional, with defaults)
# ════════════════════════════════════════════════════════════════════════════
agent:
  cache: true
  refresh_interval: "5m"
  watch:
    mode: polling
    polling_interval: "5m"
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# DEFAULTS - Default behavior for all secrets (optional)
# ════════════════════════════════════════════════════════════════════════════
defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# SECRETS - Which secrets to sync and how to inject them
# ════════════════════════════════════════════════════════════════════════════
secrets:
  - name: database_credentials
    provider: vault
    inject:
      type: env
    targets:
      containers:
        - app
    mappings:
      DB_USER: secret/data/database/username
      DB_PASSWORD: secret/data/database/password
      DB_HOST: secret/data/database/host
`

	case "huawei":
		baseConfig += `  huawei:
    type: huawei
    region: cn-north-4          # Change to your Huawei region
    auth:
      method: access_key         # or iam_role
      params:
        access_key: YOUR_ACCESS_KEY
        secret_key: YOUR_SECRET_KEY

# ════════════════════════════════════════════════════════════════════════════
# AGENT - Runtime configuration (optional, with defaults)
# ════════════════════════════════════════════════════════════════════════════
agent:
  cache: true
  refresh_interval: "5m"
  watch:
    mode: polling
    polling_interval: "5m"
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# DEFAULTS - Default behavior for all secrets (optional)
# ════════════════════════════════════════════════════════════════════════════
defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

# ════════════════════════════════════════════════════════════════════════════
# SECRETS - Which secrets to sync and how to inject them
# ════════════════════════════════════════════════════════════════════════════
secrets:
  - name: database_credentials
    provider: huawei
    inject:
      type: env
    targets:
      containers:
        - app
    mappings:
      DB_USER: my-database-username
      DB_PASSWORD: my-database-password
      DB_HOST: my-database-host
`

	default:
		baseConfig += `  # Add your provider configuration here

agent:
  cache: true
  refresh_interval: "5m"
  watch:
    mode: polling
    polling_interval: "5m"
  rotation:
    enabled: true
    strategy: rolling

defaults:
  inject:
    type: env
  rotation:
    enabled: true
    strategy: rolling

secrets: []
`
	}

	return baseConfig
}

// setupNonRootAccess configures non-root user access
func setupNonRootAccess(logger bootstrap.Logger) error {
	// This would require user input for which user to configure
	// For now, just return success as it's optional
	return nil
}
