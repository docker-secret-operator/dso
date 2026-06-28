package setup

import (
	"context"
	"os"
)

// ProviderChecks covers DSO-DOCTOR-010 through DSO-DOCTOR-011.
type ProviderChecks struct {
	provider  string
	lookupEnv func(string) string
}

func newProviderChecks(provider string) *ProviderChecks {
	return &ProviderChecks{
		provider:  provider,
		lookupEnv: os.Getenv,
	}
}

func (pc *ProviderChecks) run(_ context.Context) []DoctorCheck {
	return []DoctorCheck{
		pc.checkProviderKnown(),
		pc.checkCredentials(),
	}
}

// DSO-DOCTOR-010: Provider type is recognised.
func (pc *ProviderChecks) checkProviderKnown() DoctorCheck {
	const id = "DSO-DOCTOR-010"
	const name = "Provider type"
	const desc = "Secret provider must be one of: local, aws, vault, azure"

	known := map[string]bool{"local": true, "aws": true, "vault": true, "azure": true}
	p := pc.provider
	if p == "" {
		return infoCheck(id, name, desc,
			"no provider configured — defaulting to local mode",
			DoctorCatProvider,
		)
	}
	if !known[p] {
		return failCheck(id, name, desc,
			"unknown provider: "+p,
			"Provider name is not recognised by DSO",
			DoctorHigh, DoctorCatProvider,
			"Valid providers: local, aws, vault, azure",
			"Update the provider field in your DSO config",
		)
	}
	return passCheck(id, name, desc, "provider '"+p+"' is recognised", DoctorCatProvider)
}

// DSO-DOCTOR-011: Required credentials are present for the configured provider.
func (pc *ProviderChecks) checkCredentials() DoctorCheck {
	const id = "DSO-DOCTOR-011"
	const name = "Provider credentials"

	switch pc.provider {
	case "", "local":
		return infoCheck(id, name,
			"Local provider requires no credentials",
			"local mode — no credentials required",
			DoctorCatProvider,
		)

	case "aws":
		return pc.checkAWSCredentials(id)

	case "vault":
		return pc.checkVaultCredentials(id)

	case "azure":
		return pc.checkAzureCredentials(id)

	default:
		return infoCheck(id, name,
			"Credential check for provider "+pc.provider,
			"unknown provider — skipping credential check",
			DoctorCatProvider,
		)
	}
}

func (pc *ProviderChecks) checkAWSCredentials(id string) DoctorCheck {
	const name = "AWS credentials"
	const desc = "AWS provider requires AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY, a shared credentials file (~/.aws/credentials), or an IAM role"

	hasStatic := pc.lookupEnv("AWS_ACCESS_KEY_ID") != "" && pc.lookupEnv("AWS_SECRET_ACCESS_KEY") != ""
	hasProfile := pc.lookupEnv("AWS_PROFILE") != "" || pc.lookupEnv("AWS_DEFAULT_PROFILE") != ""
	hasRole := pc.lookupEnv("AWS_ROLE_ARN") != "" || pc.lookupEnv("AWS_WEB_IDENTITY_TOKEN_FILE") != ""

	if !hasStatic && !hasProfile && !hasRole {
		return failCheck(id, name, desc,
			"no AWS credentials found in environment",
			"DSO cannot authenticate with AWS Secrets Manager",
			DoctorHigh, DoctorCatProvider,
			"Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY",
			"Or configure an IAM role (AWS_ROLE_ARN + AWS_WEB_IDENTITY_TOKEN_FILE)",
			"Or set AWS_PROFILE to reference a profile in ~/.aws/credentials",
		)
	}
	return passCheck(id, name, desc, "AWS credentials found in environment", DoctorCatProvider)
}

func (pc *ProviderChecks) checkVaultCredentials(id string) DoctorCheck {
	const name = "Vault credentials"
	const desc = "Vault provider requires VAULT_ADDR and VAULT_TOKEN (or role-based auth)"

	addr := pc.lookupEnv("VAULT_ADDR")
	token := pc.lookupEnv("VAULT_TOKEN")
	roleID := pc.lookupEnv("VAULT_ROLE_ID")

	if addr == "" {
		return failCheck(id, name, desc,
			"VAULT_ADDR not set",
			"DSO cannot connect to Vault without the server address",
			DoctorHigh, DoctorCatProvider,
			"Set VAULT_ADDR=https://your-vault-server:8200",
		)
	}
	if token == "" && roleID == "" {
		return failCheck(id, name, desc,
			"neither VAULT_TOKEN nor VAULT_ROLE_ID is set",
			"DSO cannot authenticate with Vault",
			DoctorHigh, DoctorCatProvider,
			"Set VAULT_TOKEN for token-based auth",
			"Or set VAULT_ROLE_ID + VAULT_SECRET_ID for AppRole auth",
		)
	}
	return passCheck(id, name, desc, "VAULT_ADDR and auth credentials found", DoctorCatProvider)
}

func (pc *ProviderChecks) checkAzureCredentials(id string) DoctorCheck {
	const name = "Azure credentials"
	const desc = "Azure provider requires AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, and AZURE_TENANT_ID"

	clientID := pc.lookupEnv("AZURE_CLIENT_ID")
	clientSecret := pc.lookupEnv("AZURE_CLIENT_SECRET")
	tenantID := pc.lookupEnv("AZURE_TENANT_ID")

	var missing []string
	if clientID == "" {
		missing = append(missing, "AZURE_CLIENT_ID")
	}
	if clientSecret == "" {
		missing = append(missing, "AZURE_CLIENT_SECRET")
	}
	if tenantID == "" {
		missing = append(missing, "AZURE_TENANT_ID")
	}

	if len(missing) > 0 {
		return failCheck(id, name, desc,
			"missing Azure credentials: "+joinStrings(missing),
			"DSO cannot authenticate with Azure Key Vault",
			DoctorHigh, DoctorCatProvider,
			"Set all three: AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_TENANT_ID",
		)
	}
	return passCheck(id, name, desc, "Azure credentials found in environment", DoctorCatProvider)
}

func joinStrings(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
