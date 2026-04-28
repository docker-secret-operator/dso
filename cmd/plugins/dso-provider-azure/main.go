package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

// AzureProvider implements api.SecretProvider backed by Azure Key Vault.
// Authentication uses DefaultAzureCredential which supports:
//   - az login (developer workstation)
//   - AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_TENANT_ID (service principal)
//   - Managed Identity (Azure VMs, Container Instances, App Service)
type AzureProvider struct {
	client *azsecrets.Client
}

func (p *AzureProvider) Init(cfg map[string]string) error {
	vaultURL, ok := cfg["vault_url"]
	if !ok || vaultURL == "" {
		return fmt.Errorf(
			"azure provider requires 'vault_url' in dso.yaml config\n" +
				"  Example: vault_url: \"https://my-vault.vault.azure.net/\"",
		)
	}

	// DefaultAzureCredential automatically discovers credentials from:
	// az login, AZURE_* env vars, or Managed Identity — no manual config needed.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf(
			"failed to obtain Azure credentials: %w\n"+
				"  Fix: Run 'az login', set AZURE_CLIENT_ID/AZURE_CLIENT_SECRET/AZURE_TENANT_ID,\n"+
				"       or ensure the host has a Managed Identity assigned",
			err,
		)
	}

	client, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return fmt.Errorf(
			"failed to create Azure Key Vault client for '%s': %w",
			vaultURL, err,
		)
	}

	p.client = client
	return nil
}

func (p *AzureProvider) GetSecret(name string) (map[string]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("azure provider not initialized — Init() was not called")
	}

	// Azure Key Vault does not allow underscores in secret names.
	// Automatically translate to hyphens so dso.yaml names stay readable.
	azureName := strings.ReplaceAll(name, "_", "-")

	resp, err := p.client.GetSecret(context.TODO(), azureName, "", nil)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch secret '%s' (Azure name: '%s') from Azure Key Vault: %w\n"+
				"  Fix: Verify the secret name exists in the vault and the identity has 'Key Vault Secrets User' role",
			name, azureName, err,
		)
	}

	if resp.Value == nil {
		return nil, fmt.Errorf("azure secret '%s' has no value", name)
	}

	// Try JSON decode; if the value is a plain string, wrap it under "value".
	var data map[string]string
	if err := json.Unmarshal([]byte(*resp.Value), &data); err != nil {
		return map[string]string{"value": *resp.Value}, nil
	}

	return data, nil
}

func (p *AzureProvider) WatchSecret(name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		send := func() {
			val, err := p.GetSecret(name)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			ch <- api.SecretUpdate{Name: name, Data: val, Error: errMsg}
		}
		send() // deliver immediately on first call

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			send()
		}
	}()
	return ch, nil
}

func main() {
	// --version support: used by `dso system doctor` and `dso system setup`
	// to validate the plugin binary is functioning correctly.
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("dso-provider-azure %s\n", version)
		os.Exit(0)
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: provider.Handshake,
		Plugins: map[string]plugin.Plugin{
			"provider": &provider.SecretProviderPlugin{Impl: &AzureProvider{}},
		},
	})
}
