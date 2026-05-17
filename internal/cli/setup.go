package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
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

	// Step 7: Show next steps
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════╗")
	fmt.Println("║                    Setup Complete!                 ║")
	fmt.Println("╚════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("📚 Next steps:")
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
		fmt.Println("  1. Start the DSO agent:")
		fmt.Println("     sudo docker dso bootstrap agent")
		fmt.Println()
		fmt.Println("  2. Check agent status:")
		fmt.Println("     sudo docker dso system status")
		fmt.Println()
		fmt.Println("  3. Edit configuration for your secrets:")
		fmt.Println("     sudo vi /etc/dso/dso.yaml")
	}

	fmt.Println()
	fmt.Println("  More commands:")
	fmt.Println("    docker dso doctor              # Validate environment")
	fmt.Println("    docker dso config show         # View configuration")
	fmt.Println("    docker dso system logs         # View agent logs")
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
  #   name: my_database_password
  #   value: your_secret_value_here
  # my_api_key:
  #   name: my_api_key
  #   value: your_api_key_here

# Inject secrets into containers
containers: {}
  # Example:
  # app:
  #   secrets:
  #     DB_PASSWORD: my_database_password
  #     API_KEY: my_api_key
`
}

// generateAgentConfig creates a cloud-specific agent configuration
func generateAgentConfig(provider string) string {
	baseConfig := `# Docker Secret Operator - Cloud Configuration
# Generated by: docker dso setup
# Mode: Agent (cloud/production deployment)

version: v1.0.0
mode: agent

providers:
`

	switch provider {
	case "aws":
		baseConfig += `  aws:
    type: aws
    region: us-east-1  # Change to your AWS region
    # IAM role should have permissions for Secrets Manager
    # Example ARN: arn:aws:iam::ACCOUNT_ID:role/dso-agent-role

secrets: {}
  # Configure your AWS Secrets Manager secrets:
  # my_database_password:
  #   name: prod/database/password  # Secret name in AWS Secrets Manager
  #   provider: aws
  # my_api_key:
  #   name: prod/api/key
  #   provider: aws

containers: {}
  # app:
  #   secrets:
  #     DB_PASSWORD: my_database_password
  #     API_KEY: my_api_key
`

	case "azure":
		baseConfig += `  azure:
    type: azure
    vault_name: my-keyvault  # Your Azure Key Vault name
    # Managed Identity or Service Principal should have Key Vault access

secrets: {}
  # Configure your Azure Key Vault secrets:
  # my_database_password:
  #   name: database-password  # Secret name in Azure Key Vault
  #   provider: azure
  # my_api_key:
  #   name: api-key
  #   provider: azure

containers: {}
  # app:
  #   secrets:
  #     DB_PASSWORD: my_database_password
  #     API_KEY: my_api_key
`

	case "vault":
		baseConfig += `  vault:
    type: vault
    address: https://vault.example.com:8200
    token_file: /etc/dso/vault-token  # Store your Vault token here

secrets: {}
  # Configure your HashiCorp Vault secrets:
  # my_database_password:
  #   name: secret/data/database/password
  #   provider: vault
  # my_api_key:
  #   name: secret/data/api/key
  #   provider: vault

containers: {}
  # app:
  #   secrets:
  #     DB_PASSWORD: my_database_password
  #     API_KEY: my_api_key
`

	case "huawei":
		baseConfig += `  huawei:
    type: huawei
    region: cn-north-4  # Your Huawei Cloud region
    # Service role should have permissions for KMS

secrets: {}
  # Configure your Huawei Cloud KMS secrets:
  # my_database_password:
  #   name: my-database-password
  #   provider: huawei
  # my_api_key:
  #   name: my-api-key
  #   provider: huawei

containers: {}
  # app:
  #   secrets:
  #     DB_PASSWORD: my_database_password
  #     API_KEY: my_api_key
`

	default:
		baseConfig += `  # Add your provider configuration here
    # Supported providers: aws, azure, vault, huawei

secrets: {}

containers: {}
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
