package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
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

// GetSecret satisfies api.SecretProvider. It delegates to getSecret with a
// background context.
func (p *AWSProvider) GetSecret(name string) (map[string]string, error) {
	return p.getSecret(context.Background(), name)
}

// GetSecretWithContext satisfies api.SecretProviderWithContext, letting the
// daemon's deadline bound the AWS SDK call rather than the plugin continuing
// to burn a request the daemon has already stopped waiting for.
func (p *AWSProvider) GetSecretWithContext(ctx context.Context, name string) (map[string]string, error) {
	return p.getSecret(ctx, name)
}

func (p *AWSProvider) getSecret(ctx context.Context, name string) (map[string]string, error) {
	if p.client == nil {
		return nil, fmt.Errorf("aws provider not initialized — Init() was not called")
	}

	input := &secretsmanager.GetSecretValueInput{
		SecretId: &name,
	}

	result, err := p.client.GetSecretValue(ctx, input)
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

	// A secret body of literal `null` or `{}` unmarshals successfully but
	// leaves data nil/empty. Rejecting it here does two things: it stops an
	// empty secret being injected into containers as if the fetch succeeded,
	// and it prevents the tag loop below from panicking with "assignment to
	// entry in nil map" (which would kill the plugin process and tear down
	// the RPC connection).
	if len(data) == 0 {
		return nil, fmt.Errorf(
			"AWS secret '%s' decoded to no key/value pairs (body was %q)\n  Fix: store the secret as a JSON object or a non-empty string",
			name, *result.SecretString,
		)
	}

	// Attach AWS resource tags as _TAG_<key> metadata fields (non-blocking).
	//
	// Tag data must never overwrite secret data. Tags share this map's
	// namespace, and in AWS they are typically writable by a broader IAM
	// population than secretsmanager:GetSecretValue readers -- so someone able
	// to set a tag named "_TAG_password" (or literally "password", once
	// prefixed) must not be able to shadow a real secret key. Existing keys
	// therefore win, and the collision is reported rather than applied
	// silently.
	descInput := &secretsmanager.DescribeSecretInput{SecretId: &name}
	if descResult, err := p.client.DescribeSecret(ctx, descInput); err == nil {
		mergeTags(data, descResult.Tags, name)
	}

	return data, nil
}

// mergeTags copies AWS resource tags into the secret map as _TAG_<key>
// entries, without ever overwriting existing secret data.
//
// Tags share the secret's key namespace, and in AWS they are typically
// writable by a broader IAM population than secretsmanager:GetSecretValue
// readers. Letting a tag win would mean someone able to set a tag named
// "password" (which becomes "_TAG_password"), or literally "_TAG_password",
// could shadow real secret material. Existing keys therefore take precedence
// and the collision is reported on stderr rather than applied silently.
//
// Extracted from GetSecret so this precedence rule is unit-testable without
// an AWS client.
func mergeTags(data map[string]string, tags []types.Tag, secretName string) {
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		key := "_TAG_" + *tag.Key
		if _, clash := data[key]; clash {
			fmt.Fprintf(os.Stderr,
				"[dso-provider-aws] ignoring tag %q on secret %q: key %q already present in the secret payload (secret data takes precedence)\n",
				*tag.Key, secretName, key)
			continue
		}
		data[key] = *tag.Value
	}
}

func (p *AWSProvider) WatchSecret(ctx context.Context, name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		defer close(ch)
		// Deliver immediately on first call so callers don't block on first tick.
		send := func() {
			val, err := p.GetSecret(name)
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			select {
			case ch <- api.SecretUpdate{Name: name, Data: val, Error: errMsg}:
			case <-ctx.Done():
				return
			}
		}

		// Check context before initial send
		select {
		case <-ctx.Done():
			return
		default:
		}
		send()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				send()
			}
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
