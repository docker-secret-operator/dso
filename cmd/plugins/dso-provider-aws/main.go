package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

// AWSProvider implements api.SecretProvider backed by AWS Secrets Manager.
// Authentication uses the standard AWS credential chain:
//   - dso.yaml config keys: region
//   - Environment variables: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION
//   - EC2/ECS Instance Metadata Service (IAM role — recommended for production)
type AWSProvider struct {
	client *secretsmanager.Client
}

func (p *AWSProvider) Init(cfg map[string]string) error {
	opts := []func(*config.LoadOptions) error{}

	// If region is specified in dso.yaml, use it; otherwise fall back to
	// the standard AWS_REGION / AWS_DEFAULT_REGION environment variables.
	if region, ok := cfg["region"]; ok && region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		return fmt.Errorf(
			"failed to load AWS config: %w\n  Fix: Ensure AWS credentials are available via environment variables, IAM role, or ~/.aws/credentials",
			err,
		)
	}

	p.client = secretsmanager.NewFromConfig(awsCfg)
	return nil
}

func (p *AWSProvider) GetSecret(name string) (map[string]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("aws provider not initialized — Init() was not called")
	}

	input := &secretsmanager.GetSecretValueInput{
		SecretId: &name,
	}

	result, err := p.client.GetSecretValue(context.TODO(), input)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch secret '%s' from AWS Secrets Manager: %w\n  Fix: Verify the secret name and IAM permissions (secretsmanager:GetSecretValue)",
			name, err,
		)
	}

	if result.SecretString == nil {
		return nil, fmt.Errorf(
			"AWS secret '%s' has no string value (binary secrets are not supported)",
			name,
		)
	}

	// Try JSON decode first; if the secret is a flat string, wrap it under "value".
	var data map[string]string
	if err := json.Unmarshal([]byte(*result.SecretString), &data); err != nil {
		data = map[string]string{"value": *result.SecretString}
	}

	// Attach AWS resource tags as _TAG_<key> metadata fields (non-blocking).
	descInput := &secretsmanager.DescribeSecretInput{SecretId: &name}
	if descResult, err := p.client.DescribeSecret(context.TODO(), descInput); err == nil && descResult.Tags != nil {
		for _, tag := range descResult.Tags {
			if tag.Key != nil && tag.Value != nil {
				data["_TAG_"+*tag.Key] = *tag.Value
			}
		}
	}

	return data, nil
}

func (p *AWSProvider) WatchSecret(name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		// Deliver immediately on first call so callers don't block on first tick.
		send := func() {
			val, err := p.GetSecret(name)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			ch <- api.SecretUpdate{Name: name, Data: val, Error: errMsg}
		}
		send()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			send()
		}
	}()
	return ch, nil
}

func main() {
	// --version support: used by `docker dso system doctor` and `docker dso system setup`
	// to validate the plugin binary is functioning correctly.
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("dso-provider-aws %s\n", version)
		os.Exit(0)
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: provider.Handshake,
		Plugins: map[string]plugin.Plugin{
			"provider": &provider.SecretProviderPlugin{Impl: &AWSProvider{}},
		},
	})
}
