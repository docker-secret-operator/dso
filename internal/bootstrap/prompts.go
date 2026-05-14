package bootstrap

import (
	"bufio"
	"fmt"
	"golang.org/x/term"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// InteractivePrompter handles user interaction for bootstrap configuration
type InteractivePrompter struct {
	stdin  io.Reader
	stdout io.Writer
	logger Logger
}

// NewInteractivePrompter creates a new interactive prompter
func NewInteractivePrompter(logger Logger) *InteractivePrompter {
	return &InteractivePrompter{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		logger: logger,
	}
}

// PromptBootstrapMode asks user to choose bootstrap mode
func (p *InteractivePrompter) PromptBootstrapMode() (BootstrapMode, error) {
	fmt.Fprintf(p.stdout, "\nDSO Bootstrap Mode Selection\n")
	fmt.Fprintf(p.stdout, "===========================\n\n")
	fmt.Fprintf(p.stdout, "1. Local   - For local development/testing (stores config locally, no systemd)\n")
	fmt.Fprintf(p.stdout, "2. Agent   - For production cloud deployment (systemd managed, auto-rotate)\n\n")

	response, err := p.promptInput("Select mode (1 or 2)")
	if err != nil {
		return "", err
	}

	response = strings.TrimSpace(response)
	switch response {
	case "1":
		return ModeLocal, nil
	case "2":
		return ModeAgent, nil
	default:
		return "", fmt.Errorf("invalid mode selection: %s", response)
	}
}

// PromptCloudProviderConfirmation asks user to confirm detected cloud provider
func (p *InteractivePrompter) PromptCloudProviderConfirmation(detected *CloudProviderInfo) (bool, error) {
	if !detected.Detected {
		fmt.Fprintf(p.stdout, "\nNo cloud provider detected. Running in local mode.\n")
		return false, nil
	}

	fmt.Fprintf(p.stdout, "\nCloud Provider Detection\n")
	fmt.Fprintf(p.stdout, "========================\n\n")
	fmt.Fprintf(p.stdout, "Detected: %s\n\n", detected.Provider)

	response, err := p.promptInput("Use detected cloud provider? (yes/no)")
	if err != nil {
		return false, err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "yes" || response == "y", nil
}

// PromptProviderSelection asks user to select a cloud provider
func (p *InteractivePrompter) PromptProviderSelection() (string, error) {
	fmt.Fprintf(p.stdout, "\nCloud Provider Selection\n")
	fmt.Fprintf(p.stdout, "========================\n\n")
	fmt.Fprintf(p.stdout, "1. AWS     - Amazon Web Services (EC2 IAM roles)\n")
	fmt.Fprintf(p.stdout, "2. Azure   - Microsoft Azure (Key Vault)\n")
	fmt.Fprintf(p.stdout, "3. Huawei  - Huawei Cloud (KMS)\n")
	fmt.Fprintf(p.stdout, "4. Vault   - HashiCorp Vault (self-hosted)\n")
	fmt.Fprintf(p.stdout, "5. None    - No cloud provider (local secrets)\n\n")

	response, err := p.promptInput("Select provider (1-5)")
	if err != nil {
		return "", err
	}

	response = strings.TrimSpace(response)
	switch response {
	case "1":
		return ProviderAWS, nil
	case "2":
		return ProviderAzure, nil
	case "3":
		return ProviderHuawei, nil
	case "4":
		return ProviderVault, nil
	case "5":
		return "", nil // No provider
	default:
		return "", fmt.Errorf("invalid provider selection: %s", response)
	}
}

// PromptAWSRegion asks user for AWS region
func (p *InteractivePrompter) PromptAWSRegion() (string, error) {
	fmt.Fprintf(p.stdout, "\nAWS Configuration\n")
	fmt.Fprintf(p.stdout, "=================\n\n")
	fmt.Fprintf(p.stdout, "Common regions: us-east-1, us-west-2, eu-west-1, ap-southeast-1\n\n")

	region, err := p.promptInput("Enter AWS region")
	if err != nil {
		return "", err
	}

	region = strings.TrimSpace(region)
	if region == "" {
		return "", fmt.Errorf("region cannot be empty")
	}

	return region, nil
}

// PromptAzureVaultURL asks user for Azure Key Vault URL
func (p *InteractivePrompter) PromptAzureVaultURL() (string, error) {
	fmt.Fprintf(p.stdout, "\nAzure Configuration\n")
	fmt.Fprintf(p.stdout, "===================\n\n")
	fmt.Fprintf(p.stdout, "Example: https://my-vault.vault.azure.net/\n\n")

	for {
		urlStr, err := p.promptInput("Enter Azure Key Vault URL")
		if err != nil {
			return "", err
		}

		urlStr = strings.TrimSpace(urlStr)

		// Validate URL format
		if err := p.validateURL(urlStr); err != nil {
			fmt.Fprintf(p.stdout, "Error: %v. Please try again.\n", err)
			continue
		}

		// Ensure it's HTTPS for Azure Key Vault
		if !strings.HasPrefix(urlStr, "https://") && !strings.HasPrefix(urlStr, "http://") {
			fmt.Fprintf(p.stdout, "Error: URL must start with http:// or https://. Please try again.\n")
			continue
		}

		// Check it looks like an Azure vault URL
		if !strings.Contains(urlStr, "vault.azure.net") {
			fmt.Fprintf(p.stdout, "Warning: URL doesn't look like an Azure Key Vault URL (should contain 'vault.azure.net')\n")
		}

		return urlStr, nil
	}
}

// PromptHuaweiRegionAndProject asks user for Huawei region and project ID
func (p *InteractivePrompter) PromptHuaweiRegionAndProject() (region, projectID string, err error) {
	fmt.Fprintf(p.stdout, "\nHuawei Cloud Configuration\n")
	fmt.Fprintf(p.stdout, "==========================\n\n")
	fmt.Fprintf(p.stdout, "Common regions: ap-southeast-1, ap-southeast-2, cn-north-1\n\n")

	region, err = p.promptInput("Enter Huawei region")
	if err != nil {
		return "", "", err
	}

	region = strings.TrimSpace(region)
	if region == "" {
		return "", "", fmt.Errorf("region cannot be empty")
	}

	projectID, err = p.promptInput("Enter Huawei Project ID")
	if err != nil {
		return "", "", err
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", "", fmt.Errorf("project ID cannot be empty")
	}

	return region, projectID, nil
}

// PromptVaultAddress asks user for Vault server address
func (p *InteractivePrompter) PromptVaultAddress() (string, error) {
	fmt.Fprintf(p.stdout, "\nHashiCorp Vault Configuration\n")
	fmt.Fprintf(p.stdout, "==============================\n\n")
	fmt.Fprintf(p.stdout, "Example: http://vault.internal:8200\n\n")

	for {
		address, err := p.promptInput("Enter Vault server address")
		if err != nil {
			return "", err
		}

		address = strings.TrimSpace(address)

		// Validate URL format
		if err := p.validateURL(address); err != nil {
			fmt.Fprintf(p.stdout, "Error: %v. Please try again.\n", err)
			continue
		}

		// Ensure it has http:// or https://
		if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
			fmt.Fprintf(p.stdout, "Error: Address must start with http:// or https://. Please try again.\n")
			continue
		}

		return address, nil
	}
}

// PromptSecrets asks user to define secrets
func (p *InteractivePrompter) PromptSecrets(provider string) ([]SecretDefinition, error) {
	var secrets []SecretDefinition
	seenSecrets := make(map[string]bool) // Track secret names to prevent duplicates

	fmt.Fprintf(p.stdout, "\nSecret Definition\n")
	fmt.Fprintf(p.stdout, "=================\n\n")

	for {
		secretName, err := p.promptInput("Enter secret name (or 'done' to finish)")
		if err != nil {
			return nil, err
		}

		secretName = strings.TrimSpace(secretName)
		if secretName == "done" {
			break
		}

		// Validate secret name
		if err := p.validateSecretName(secretName); err != nil {
			fmt.Fprintf(p.stdout, "Error: %v. Please try again.\n", err)
			continue
		}

		// Check for duplicates
		if seenSecrets[secretName] {
			fmt.Fprintf(p.stdout, "Error: Secret '%s' already defined. Please use a different name.\n", secretName)
			continue
		}

		// Display provider-specific prompt
		switch provider {
		case ProviderAWS:
			fmt.Fprintf(p.stdout, "Example: arn:aws:secretsmanager:region:account:secret:name\n")
		case ProviderAzure:
			fmt.Fprintf(p.stdout, "Example: secret-name (without vault URL)\n")
		case ProviderHuawei:
			fmt.Fprintf(p.stdout, "Example: kms-secret-name\n")
		case ProviderVault:
			fmt.Fprintf(p.stdout, "Example: path/to/secret\n")
		}

		// Get key mappings
		mappings := make(map[string]string)
		for {
			mapping, err := p.promptInput("Enter environment variable mapping or 'next' to move to next secret")
			if err != nil {
				return nil, err
			}

			mapping = strings.TrimSpace(mapping)
			if mapping == "next" || mapping == "" {
				break
			}

			// Parse mapping as KEY=VALUE
			parts := strings.Split(mapping, "=")
			if len(parts) != 2 {
				fmt.Fprintf(p.stdout, "Invalid format. Use: SECRET_KEY=ENV_VAR\n")
				continue
			}

			secretKey := strings.TrimSpace(parts[0])
			envVar := strings.TrimSpace(parts[1])

			if secretKey == "" || envVar == "" {
				fmt.Fprintf(p.stdout, "Key and value cannot be empty\n")
				continue
			}

			// Validate environment variable name
			if err := p.validateEnvVarName(envVar); err != nil {
				fmt.Fprintf(p.stdout, "Error: %v. Please try again.\n", err)
				continue
			}

			mappings[secretKey] = envVar
		}

		if len(mappings) == 0 {
			fmt.Fprintf(p.stdout, "Secret must have at least one mapping, skipping\n")
			continue
		}

		// Mark this secret as seen
		seenSecrets[secretName] = true

		secrets = append(secrets, SecretDefinition{
			Name:     secretName,
			Provider: provider,
			Mappings: mappings,
		})

		fmt.Fprintf(p.stdout, "Secret '%s' added with %d mappings\n\n", secretName, len(mappings))
	}

	if len(secrets) == 0 {
		fmt.Fprintf(p.stdout, "Warning: No secrets defined\n")
	}

	return secrets, nil
}

// PromptSecureInput prompts for sensitive input (hidden from terminal)
func (p *InteractivePrompter) PromptSecureInput(prompt string) (string, error) {
	fmt.Fprint(p.stdout, prompt)

	// Read password without echo
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", ErrInteractivePrompt("prompts", err)
	}

	fmt.Fprintf(p.stdout, "\n")
	return string(bytes), nil
}

// PromptConfirmation asks user for yes/no confirmation
func (p *InteractivePrompter) PromptConfirmation(prompt string) (bool, error) {
	response, err := p.promptInput(prompt + " (yes/no)")
	if err != nil {
		return false, err
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "yes" || response == "y", nil
}

// PromptConfigPath asks user for configuration file path
func (p *InteractivePrompter) PromptConfigPath() (string, error) {
	fmt.Fprintf(p.stdout, "\nConfiguration File Path\n")
	fmt.Fprintf(p.stdout, "=======================\n\n")
	fmt.Fprintf(p.stdout, "Default: /etc/dso/dso.yaml\n\n")

	path, err := p.promptInput("Enter configuration path (or press Enter for default)")
	if err != nil {
		return "", err
	}

	path = strings.TrimSpace(path)
	if path == "" {
		path = "/etc/dso/dso.yaml"
	}

	return path, nil
}

// DisplaySummary displays configuration summary to user
func (p *InteractivePrompter) DisplaySummary(mode BootstrapMode, provider string, config *Config) error {
	fmt.Fprintf(p.stdout, "\nBootstrap Configuration Summary\n")
	fmt.Fprintf(p.stdout, "===============================\n\n")

	fmt.Fprintf(p.stdout, "Mode:     %s\n", mode)
	fmt.Fprintf(p.stdout, "Provider: %s\n", provider)
	fmt.Fprintf(p.stdout, "Providers configured: %d\n", len(config.Providers))
	fmt.Fprintf(p.stdout, "Secrets configured:   %d\n", len(config.Secrets))

	if len(config.Providers) > 0 {
		fmt.Fprintf(p.stdout, "\nProviders:\n")
		for name, prov := range config.Providers {
			fmt.Fprintf(p.stdout, "  - %s (%s)\n", name, prov.Type)
		}
	}

	if len(config.Secrets) > 0 {
		fmt.Fprintf(p.stdout, "\nSecrets:\n")
		for _, secret := range config.Secrets {
			fmt.Fprintf(p.stdout, "  - %s (provider: %s, mappings: %d)\n",
				secret.Name, secret.Provider, len(secret.Mappings))
		}
	}

	fmt.Fprintf(p.stdout, "\n")

	return nil
}

// DisplayCompletionMessage displays success message
func (p *InteractivePrompter) DisplayCompletionMessage(configPath string, mode BootstrapMode) error {
	fmt.Fprintf(p.stdout, "\n✓ Bootstrap Completed Successfully\n")
	fmt.Fprintf(p.stdout, "==================================\n\n")

	fmt.Fprintf(p.stdout, "Configuration written to: %s\n", configPath)

	if mode == ModeAgent {
		fmt.Fprintf(p.stdout, "\nNext steps:\n")
		fmt.Fprintf(p.stdout, "1. Enable DSO agent: sudo systemctl enable dso-agent\n")
		fmt.Fprintf(p.stdout, "2. Start DSO agent:  sudo systemctl start dso-agent\n")
		fmt.Fprintf(p.stdout, "3. Check status:     sudo systemctl status dso-agent\n")
		fmt.Fprintf(p.stdout, "4. View logs:        sudo journalctl -u dso-agent -f\n")

		fmt.Fprintf(p.stdout, "\nFor non-root CLI usage, run:\n")
		fmt.Fprintf(p.stdout, "1. sudo usermod -aG dso $USER\n")
		fmt.Fprintf(p.stdout, "2. sudo usermod -aG docker $USER\n")
		fmt.Fprintf(p.stdout, "3. Log out and back in\n")
	} else {
		fmt.Fprintf(p.stdout, "\nConfiguration is ready to use.\n")
	}

	fmt.Fprintf(p.stdout, "\n")

	return nil
}

// validateURL checks if a string is a valid URL
func (p *InteractivePrompter) validateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	_, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	return nil
}

// validateEnvVarName checks if a string is a valid environment variable name
// Valid names: [A-Za-z_][A-Za-z0-9_]*
func (p *InteractivePrompter) validateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("environment variable name cannot be empty")
	}

	envVarRegex := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	if !envVarRegex.MatchString(name) {
		return fmt.Errorf("invalid environment variable name '%s': must start with letter or underscore, contain only alphanumerics and underscores", name)
	}

	return nil
}

// validateSecretName checks if a string is a valid secret name
// Allows alphanumerics, hyphens, underscores, dots (common in secret naming)
func (p *InteractivePrompter) validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	// Allow alphanumerics, hyphens, underscores, dots, slashes (for paths)
	secretNameRegex := regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)
	if !secretNameRegex.MatchString(name) {
		return fmt.Errorf("invalid secret name '%s': use only alphanumerics, hyphens, underscores, dots, and slashes", name)
	}

	return nil
}

// promptInput is a helper that prompts and reads a line of input
func (p *InteractivePrompter) promptInput(prompt string) (string, error) {
	fmt.Fprintf(p.stdout, "%s: ", prompt)

	reader := bufio.NewReader(p.stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", ErrInteractivePrompt("prompts", err)
	}

	return strings.TrimSuffix(line, "\n"), nil
}
