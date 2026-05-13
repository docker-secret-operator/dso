package bootstrap

import (
	"fmt"
	"strings"
)

// ProviderConfigHandler handles provider-specific configuration logic
type ProviderConfigHandler struct {
	logger Logger
}

// NewProviderConfigHandler creates a new provider config handler
func NewProviderConfigHandler(logger Logger) *ProviderConfigHandler {
	return &ProviderConfigHandler{
		logger: logger,
	}
}

// BuildAWSProviderConfig builds an AWS provider configuration
func (pch *ProviderConfigHandler) BuildAWSProviderConfig(name, region string) ProviderConfig {
	pch.logger.Info("Building AWS provider config", "name", name, "region", region)

	return ProviderConfig{
		Type:   "aws",
		Region: region,
	}
}

// BuildAzureProviderConfig builds an Azure provider configuration
func (pch *ProviderConfigHandler) BuildAzureProviderConfig(name, vaultURL string) ProviderConfig {
	pch.logger.Info("Building Azure provider config", "name", name, "vault_url", vaultURL)

	// Extract just the vault URL (handle if user provided full secret path)
	cleanURL := strings.TrimSpace(vaultURL)
	if strings.Contains(cleanURL, "/secrets/") {
		parts := strings.Split(cleanURL, "/secrets/")
		cleanURL = parts[0] + "/"
	}

	return ProviderConfig{
		Type: "azure",
		Config: map[string]string{
			"vault_url": cleanURL,
		},
	}
}

// BuildHuaweiProviderConfig builds a Huawei provider configuration
func (pch *ProviderConfigHandler) BuildHuaweiProviderConfig(name, region, projectID string) ProviderConfig {
	pch.logger.Info("Building Huawei provider config",
		"name", name,
		"region", region,
		"project_id", projectID)

	return ProviderConfig{
		Type:   "huawei",
		Region: region,
		Config: map[string]string{
			"project_id": projectID,
		},
	}
}

// BuildVaultProviderConfig builds a Vault provider configuration
func (pch *ProviderConfigHandler) BuildVaultProviderConfig(name, address, token, mount string) ProviderConfig {
	pch.logger.Info("Building Vault provider config", "name", name, "address", address)

	config := map[string]string{
		"address": address,
	}

	// Add token if provided (may use environment variable instead)
	if token != "" {
		config["token"] = token
	}

	// Add mount if provided
	if mount != "" {
		config["mount"] = mount
	} else {
		config["mount"] = "secret" // Default
	}

	return ProviderConfig{
		Type:   "vault",
		Config: config,
	}
}

// ExtractSecretName extracts the actual secret name from provider-specific identifiers
func (pch *ProviderConfigHandler) ExtractSecretName(providerType, identifier string) string {
	switch providerType {
	case "aws":
		// For AWS ARNs like arn:aws:secretsmanager:region:account:secret:name
		// Return the last component
		parts := strings.Split(identifier, ":")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
		return identifier

	case "azure":
		// For Azure URLs like https://vault.vault.azure.net/secrets/secret-name
		// Extract the secret name
		if strings.Contains(identifier, "/secrets/") {
			parts := strings.Split(identifier, "/secrets/")
			if len(parts) > 1 {
				// Return the part after /secrets/
				name := parts[1]
				// Remove trailing slashes
				return strings.TrimRight(name, "/")
			}
		}
		return identifier

	case "huawei":
		// Huawei typically uses simple names
		return identifier

	case "vault":
		// Vault uses paths like path/to/secret
		return identifier

	default:
		return identifier
	}
}

// ValidateProviderCredentials validates that required credentials are present
func (pch *ProviderConfigHandler) ValidateProviderCredentials(providerType string, config map[string]string) error {
	switch providerType {
	case "aws":
		// AWS uses IAM roles, no explicit credentials needed in config
		return nil

	case "azure":
		if config["vault_url"] == "" {
			return fmt.Errorf("Azure requires vault_url")
		}
		return nil

	case "huawei":
		if config["project_id"] == "" {
			return fmt.Errorf("Huawei requires project_id")
		}
		return nil

	case "vault":
		if config["address"] == "" {
			return fmt.Errorf("Vault requires address")
		}
		// Token can be provided via environment variable
		return nil

	default:
		return fmt.Errorf("unknown provider type: %s", providerType)
	}
}

// GetProviderExamples returns example values for each provider
func (pch *ProviderConfigHandler) GetProviderExamples(providerType string) map[string]string {
	examples := make(map[string]string)

	switch providerType {
	case "aws":
		examples["region"] = "us-east-1"
		examples["arn_example"] = "arn:aws:secretsmanager:us-east-1:123456789:secret:my-secret"

	case "azure":
		examples["vault_url"] = "https://my-vault.vault.azure.net/"
		examples["secret_example"] = "my-secret (without vault URL)"

	case "huawei":
		examples["region"] = "ap-southeast-2"
		examples["project_id"] = "YOUR_PROJECT_ID"
		examples["secret_example"] = "kms-secret-name"

	case "vault":
		examples["address"] = "http://vault.internal:8200"
		examples["mount"] = "secret"
		examples["path_example"] = "path/to/secret"
	}

	return examples
}

// GetProviderDocumentation returns documentation for each provider
func (pch *ProviderConfigHandler) GetProviderDocumentation(providerType string) string {
	docs := make(map[string]string)

	docs["aws"] = `
AWS Provider Configuration
===========================

The AWS provider uses EC2 instance metadata service (IMDSv2) to authenticate.
No explicit credentials are needed in the configuration.

Configuration:
  type: aws
  region: us-east-1    # Required: AWS region for secrets

Secrets:
  - name: my-secret
    provider: aws-prod
    mappings:
      username: DB_USER
      password: DB_PASSWORD

Expects:
  - EC2 instance with IAM role that has access to Secrets Manager
  - Secret ARN format: arn:aws:secretsmanager:region:account:secret:name
`

	docs["azure"] = `
Azure Provider Configuration
=============================

The Azure provider uses Azure Key Vault for secret management.

Configuration:
  type: azure
  config:
    vault_url: "https://my-vault.vault.azure.net/"

Secrets:
  - name: my-secret
    provider: azure-prod
    mappings:
      username: DB_USER
      password: DB_PASSWORD

Authentication:
  - Uses Managed Identity (recommended)
  - Or service principal credentials via environment variables

Expects:
  - Azure Key Vault instance
  - Appropriate Azure roles/permissions for the identity
`

	docs["huawei"] = `
Huawei Cloud Provider Configuration
====================================

The Huawei provider uses KMS/DEW for secret management.

Configuration:
  type: huawei
  region: ap-southeast-2
  config:
    project_id: YOUR_PROJECT_ID

Secrets:
  - name: my-secret
    provider: huawei-prod
    mappings:
      username: DB_USER
      password: DB_PASSWORD

Authentication:
  - Uses cloud credentials from environment or metadata service
  - Set HUAWEI_ACCESS_KEY, HUAWEI_SECRET_KEY environment variables

Expects:
  - Huawei Cloud KMS setup
  - Proper IAM permissions for the instance
`

	docs["vault"] = `
HashiCorp Vault Provider Configuration
=======================================

The Vault provider works with self-hosted or managed Vault instances.

Configuration:
  type: vault
  config:
    address: "http://vault.internal:8200"
    token: "${VAULT_TOKEN}"              # Or use environment variable
    mount: "secret"                       # Mount path for KV secrets

Secrets:
  - name: my-secret
    provider: vault-dev
    mappings:
      username: DB_USER
      password: DB_PASSWORD

Authentication:
  - Token-based (set VAULT_TOKEN environment variable)
  - Kubernetes auth
  - AWS auth
  - AppRole

Expects:
  - Running Vault instance
  - Authentication configured
  - KV secrets at specified paths
`

	if doc, exists := docs[providerType]; exists {
		return doc
	}
	return "No documentation for provider: " + providerType
}

// GetProviderComparisonTable returns a comparison of all providers
func (pch *ProviderConfigHandler) GetProviderComparisonTable() string {
	return `
Provider Comparison
===================

Provider | Best For                    | Setup Complexity | Cloud Lock-in
---------|------------------------------|------------------|---------------
AWS      | AWS deployments              | Low              | High
Azure    | Azure deployments            | Low              | High
Huawei   | Huawei Cloud deployments     | Medium           | High
Vault    | Multi-cloud, self-hosted     | Medium-High      | None

Provider | Authentication       | Credential Rotation | Multi-region
---------|---------------------|-------------------|---------------
AWS      | IAM roles           | Built-in           | Yes
Azure    | Managed Identity    | Built-in           | Yes
Huawei   | Cloud credentials   | Manual             | Yes
Vault    | Multiple methods    | Manual             | Yes
`
}
