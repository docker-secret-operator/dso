package setup

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// validateProvider checks that the requested (or auto-selected) provider has
// credentials and, when a connectivity checker is configured, that the
// provider endpoint is reachable. It never calls LookPath, os.Stat, or any
// environment-discovery primitive — those belong exclusively to the detector.
func (v *Validator) validateProvider(ctx context.Context, env Environment, opts SetupOptions) ([]ValidationError, []ValidationWarning, error) {
	provider := opts.Provider
	if provider == "" {
		// No explicit provider. Warn when only local secrets are available so
		// the user knows cloud integration hasn't been configured.
		if len(env.Providers.Available) == 1 && env.Providers.Available[0] == "local" {
			return nil, []ValidationWarning{{
				Code:    "no_cloud_provider_detected",
				Message: "no cloud provider credentials detected; DSO will use locally stored secrets only",
			}}, nil
		}
		return nil, nil, nil
	}

	switch provider {
	case "local":
		return nil, nil, nil
	case "aws":
		errs, warns, err := v.validateAWS(ctx, env.Providers.AWS)
		return errs, warns, err
	case "vault":
		errs, warns, err := v.validateVault(ctx, env.Providers.Vault)
		return errs, warns, err
	case "azure":
		errs, warns, err := v.validateAzure(ctx, env.Providers.Azure)
		return errs, warns, err
	default:
		return []ValidationError{{
			Code:    "unknown_provider",
			Message: fmt.Sprintf("unknown provider %q — valid values: aws, vault, azure, local", provider),
			Recovery: []string{
				"Use one of: dso setup --provider aws|vault|azure|local",
			},
		}}, nil, nil
	}
}

func (v *Validator) validateAWS(ctx context.Context, info AWSInfo) ([]ValidationError, []ValidationWarning, error) {
	if !info.Detected {
		return []ValidationError{{
			Code:    "aws_credentials_missing",
			Message: "AWS provider selected but no credentials were detected",
			Recovery: []string{
				"Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY",
				"Or configure an IAM role: set AWS_ROLE_ARN",
				"Or create ~/.aws/credentials",
			},
		}}, nil, nil
	}

	if v.cfg.CheckAWSConnectivity != nil {
		if err := v.cfg.CheckAWSConnectivity(ctx, info.Region); err != nil {
			return []ValidationError{{
				Code:    "aws_connectivity_failed",
				Message: "AWS credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify the credentials are valid and not expired",
					"Check network access to AWS endpoints",
				},
			}}, nil, nil
		}
	}

	return nil, nil, nil
}

func (v *Validator) validateVault(ctx context.Context, info VaultInfo) ([]ValidationError, []ValidationWarning, error) {
	if !info.Detected {
		return []ValidationError{{
			Code:    "vault_credentials_missing",
			Message: "Vault provider selected but VAULT_ADDR and a token or role are not configured",
			Recovery: []string{
				"Set VAULT_ADDR to your Vault server address",
				"Set VAULT_TOKEN, or set VAULT_ROLE_ID for AppRole authentication",
			},
		}}, nil, nil
	}

	if v.cfg.CheckVaultConnectivity != nil {
		if err := v.cfg.CheckVaultConnectivity(ctx, info.Address); err != nil {
			return []ValidationError{{
				Code:    "vault_connectivity_failed",
				Message: "Vault credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify VAULT_ADDR is correct and the Vault cluster is unsealed",
					"Check network access to the Vault address",
				},
			}}, nil, nil
		}
	}

	return nil, nil, nil
}

func (v *Validator) validateAzure(ctx context.Context, info AzureInfo) ([]ValidationError, []ValidationWarning, error) {
	if !info.Detected {
		return []ValidationError{{
			Code:    "azure_credentials_missing",
			Message: "Azure provider selected but no credentials were detected",
			Recovery: []string{
				"Set AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, and AZURE_TENANT_ID",
				"Or install and login via Azure CLI: az login",
			},
		}}, nil, nil
	}

	if v.cfg.CheckAzureConnectivity != nil {
		if err := v.cfg.CheckAzureConnectivity(ctx); err != nil {
			return []ValidationError{{
				Code:    "azure_connectivity_failed",
				Message: "Azure credentials detected but connectivity check failed: " + err.Error(),
				Recovery: []string{
					"Verify Azure credentials are valid",
					"For CLI auth: run az login",
				},
			}}, nil, nil
		}
	}

	return nil, nil, nil
}

// ─── Real connectivity implementations ───────────────────────────────────────
// These are the default checkers used by newValidator(). Each makes a single
// lightweight HTTP probe — any HTTP response (even 4xx) means the endpoint is
// reachable. Tests inject nil (skip) or a mock function in their place.

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
// It respects ctx for cancellation; a 5-second hard ceiling guards against
// stalled connections when the caller does not set a tighter timeout.
func probeHTTPEndpoint(ctx context.Context, url string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL %q: %w", url, err)
	}

	// #nosec G107 — URL originates from trusted env vars (VAULT_ADDR, AWS region
	// config), not from user-supplied input at runtime.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	resp.Body.Close()
	// Any HTTP status code (including 4xx) means the endpoint answered.
	return nil
}
