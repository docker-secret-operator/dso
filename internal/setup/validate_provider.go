package setup

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// validateProvider checks that the requested (or auto-selected) provider has
// credentials and, when a connectivity checker is configured, verifies reachability.
// It never calls LookPath, os.Stat, or any environment-discovery primitive.
func (v *Validator) validateProvider(ctx context.Context, env Environment, opts SetupOptions) ([]ValidationIssue, error) {
	provider := opts.Provider
	if provider == "" {
		if len(env.Providers.Available) == 1 && env.Providers.Available[0] == "local" {
			return []ValidationIssue{{
				Severity: SeverityWarning,
				Category: CategoryProvider,
				Code:     CodeNoCloudProviderDetected,
				Message:  "no cloud provider credentials detected; DSO will use locally stored secrets only",
			}}, nil
		}
		return nil, nil
	}

	switch provider {
	case "local":
		return nil, nil
	case "aws":
		return v.validateAWS(ctx, env.Providers.AWS)
	case "vault":
		return v.validateVault(ctx, env.Providers.Vault)
	case "azure":
		return v.validateAzure(ctx, env.Providers.Azure)
	default:
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryProvider,
			Code:     CodeUnknownProvider,
			Message:  fmt.Sprintf("unknown provider %q — valid values: aws, vault, azure, local", provider),
			Recovery: []string{"Use one of: dso setup --provider aws|vault|azure|local"},
		}}, nil
	}
}

func (v *Validator) validateAWS(ctx context.Context, info AWSInfo) ([]ValidationIssue, error) {
	if !info.Detected {
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryProvider,
			Code:     CodeAWSCredentialsMissing,
			Message:  "AWS provider selected but no credentials were detected",
			Recovery: []string{
				"Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY",
				"Or configure an IAM role: set AWS_ROLE_ARN",
				"Or create ~/.aws/credentials",
			},
		}}, nil
	}

	if v.cfg.CheckAWSConnectivity != nil {
		if err := v.cfg.CheckAWSConnectivity(ctx, info.Region); err != nil {
			return []ValidationIssue{{
				Severity: SeverityError,
				Category: CategoryProvider,
				Code:     CodeAWSConnectivityFailed,
				Message:  "AWS credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify the credentials are valid and not expired",
					"Check network access to AWS endpoints",
				},
			}}, nil
		}
	}

	return nil, nil
}

func (v *Validator) validateVault(ctx context.Context, info VaultInfo) ([]ValidationIssue, error) {
	if !info.Detected {
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryProvider,
			Code:     CodeVaultCredentialsMissing,
			Message:  "Vault provider selected but VAULT_ADDR and a token or role are not configured",
			Recovery: []string{
				"Set VAULT_ADDR to your Vault server address",
				"Set VAULT_TOKEN, or set VAULT_ROLE_ID for AppRole authentication",
			},
		}}, nil
	}

	if v.cfg.CheckVaultConnectivity != nil {
		if err := v.cfg.CheckVaultConnectivity(ctx, info.Address); err != nil {
			return []ValidationIssue{{
				Severity: SeverityError,
				Category: CategoryProvider,
				Code:     CodeVaultConnectivityFailed,
				Message:  "Vault credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify VAULT_ADDR is correct and the Vault cluster is unsealed",
					"Check network access to the Vault address",
				},
			}}, nil
		}
	}

	return nil, nil
}

func (v *Validator) validateAzure(ctx context.Context, info AzureInfo) ([]ValidationIssue, error) {
	if !info.Detected {
		return []ValidationIssue{{
			Severity: SeverityError,
			Category: CategoryProvider,
			Code:     CodeAzureCredentialsMissing,
			Message:  "Azure provider selected but no credentials were detected",
			Recovery: []string{
				"Set AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, and AZURE_TENANT_ID",
				"Or install and login via Azure CLI: az login",
			},
		}}, nil
	}

	if v.cfg.CheckAzureConnectivity != nil {
		if err := v.cfg.CheckAzureConnectivity(ctx); err != nil {
			return []ValidationIssue{{
				Severity: SeverityError,
				Category: CategoryProvider,
				Code:     CodeAzureConnectivityFailed,
				Message:  "Azure credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify Azure credentials are valid",
					"For CLI auth: run az login",
				},
			}}, nil
		}
	}

	return nil, nil
}

// ─── Real connectivity implementations ───────────────────────────────────────

func checkAWSConnectivity(ctx context.Context, region string) error {
	endpoint := "https://sts.amazonaws.com"
	if region != "" {
		endpoint = fmt.Sprintf("https://sts.%s.amazonaws.com", region)
	}
	return probeHTTPEndpoint(ctx, endpoint)
}

func checkVaultConnectivity(ctx context.Context, addr string) error {
	return probeHTTPEndpoint(ctx, addr+"/v1/sys/health")
}

func checkAzureConnectivity(ctx context.Context) error {
	return probeHTTPEndpoint(ctx, "https://management.azure.com")
}

// probeHTTPEndpoint issues a GET and treats any HTTP response as reachable.
// A 5-second hard ceiling guards against stalled connections.
func probeHTTPEndpoint(ctx context.Context, url string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL %q: %w", url, err)
	}

	// #nosec G107 — URL comes from trusted env vars (VAULT_ADDR, AWS region config).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	resp.Body.Close()
	return nil
}
