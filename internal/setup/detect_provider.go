package setup

import (
	"os"
	"path/filepath"
)

// detectProviders probes for available secret provider credentials.
// It never validates credentials — it only checks for their presence.
func detectProviders(cfg DetectorConfig) (DetectedProviders, []DetectionWarning) {
	result := DetectedProviders{}
	result.AWS = detectAWS(cfg)
	result.Azure = detectAzure(cfg)
	result.Vault = detectVault(cfg)

	// local is always available; cloud providers prepend in priority order.
	result.Available = []string{}
	if result.AWS.Detected {
		result.Available = append(result.Available, "aws")
	}
	if result.Azure.Detected {
		result.Available = append(result.Available, "azure")
	}
	if result.Vault.Detected {
		result.Available = append(result.Available, "vault")
	}
	result.Available = append(result.Available, "local")

	return result, nil
}

func detectAWS(cfg DetectorConfig) AWSInfo {
	info := AWSInfo{}

	if cfg.Getenv("AWS_ACCESS_KEY_ID") != "" && cfg.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		info.HasStaticCreds = true
		info.Detected = true
	}

	if cfg.Getenv("AWS_ROLE_ARN") != "" || cfg.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" {
		info.HasRole = true
		info.Detected = true
	}

	// Shared credentials file (~/.aws/credentials).
	if home := homeDir(cfg); home != "" {
		if _, err := cfg.Stat(filepath.Join(home, ".aws", "credentials")); err == nil {
			info.HasSharedCreds = true
			info.Detected = true
		}
	}

	if info.Detected {
		info.Region = cfg.Getenv("AWS_REGION")
		if info.Region == "" {
			info.Region = cfg.Getenv("AWS_DEFAULT_REGION")
		}
	}

	return info
}

func detectAzure(cfg DetectorConfig) AzureInfo {
	info := AzureInfo{}

	if cfg.Getenv("AZURE_CLIENT_ID") != "" &&
		cfg.Getenv("AZURE_CLIENT_SECRET") != "" &&
		cfg.Getenv("AZURE_TENANT_ID") != "" {
		info.HasEnvCreds = true
		info.Detected = true
	}

	if _, err := cfg.LookPath("az"); err == nil {
		info.HasCLI = true
		info.Detected = true
	}

	return info
}

func detectVault(cfg DetectorConfig) VaultInfo {
	info := VaultInfo{}

	addr := cfg.Getenv("VAULT_ADDR")
	if addr == "" {
		return info
	}
	info.Address = addr

	if cfg.Getenv("VAULT_TOKEN") != "" {
		info.HasToken = true
		info.Detected = true
	}
	if cfg.Getenv("VAULT_ROLE_ID") != "" {
		info.HasRole = true
		info.Detected = true
	}

	return info
}

// homeDir resolves the user's home directory using the injected getenv, so
// tests can set HOME without touching the real environment.
func homeDir(cfg DetectorConfig) string {
	if h := cfg.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}
