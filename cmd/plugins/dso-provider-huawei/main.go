package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"

	"github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	csms "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/csms/v1"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/csms/v1/model"
	"github.com/huaweicloud/huaweicloud-sdk-go-v3/services/csms/v1/region"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

// HuaweiProvider implements api.SecretProvider backed by Huawei Cloud CSMS.
//
// Credential resolution order:
//  1. dso.yaml config keys: access_key, secret_key, security_token, project_id
//  2. Environment variables: HUAWEI_ACCESS_KEY, HUAWEI_SECRET_KEY,
//     HUAWEI_SECURITY_TOKEN, HUAWEI_REGION
//  3. Default region: ap-southeast-3
//
// For IAM Agency (ECS-attached role), supply security_token via
// /etc/dso/agent.env (EnvironmentFile in the dso-agent.service systemd unit).
type HuaweiProvider struct {
	client *csms.CsmsClient
}

func (h *HuaweiProvider) Init(cfg map[string]string) error {
	// Region resolution: dso.yaml > env var > default
	reg := cfg["region"]
	if reg == "" {
		reg = os.Getenv("HUAWEI_REGION")
	}
	if reg == "" {
		reg = "ap-southeast-3"
	}

	// Credential resolution: dso.yaml config keys > environment variables
	ak := cfg["access_key"]
	if ak == "" {
		ak = os.Getenv("HUAWEI_ACCESS_KEY")
	}
	sk := cfg["secret_key"]
	if sk == "" {
		sk = os.Getenv("HUAWEI_SECRET_KEY")
	}
	// SecurityToken is required for temporary credentials from ECS Metadata Service.
	// Retrieve with: curl http://169.254.169.254/openstack/latest/securitykey
	secToken := cfg["security_token"]
	if secToken == "" {
		secToken = os.Getenv("HUAWEI_SECURITY_TOKEN")
	}

	credBuilder := basic.NewCredentialsBuilder()
	if ak != "" {
		credBuilder = credBuilder.WithAk(ak)
	}
	if sk != "" {
		credBuilder = credBuilder.WithSk(sk)
	}
	if secToken != "" {
		credBuilder = credBuilder.WithSecurityToken(secToken)
	}
	if pid := cfg["project_id"]; pid != "" {
		credBuilder = credBuilder.WithProjectId(pid)
	}

	auth, err := credBuilder.SafeBuild()
	if err != nil {
		return fmt.Errorf("huawei Cloud credentials error: %w; fix: set access_key/secret_key in dso.yaml, set HUAWEI_ACCESS_KEY/HUAWEI_SECRET_KEY, or attach an IAM Agency and provide HUAWEI_SECURITY_TOKEN", err)
	}

	clientRegion, err := region.SafeValueOf(reg)
	if err != nil {
		return fmt.Errorf("invalid Huawei Cloud region %q: %w", reg, err)
	}

	h.client = csms.NewCsmsClient(
		csms.CsmsClientBuilder().
			WithRegion(clientRegion).
			WithCredential(auth).
			Build(),
	)
	return nil
}

func (h *HuaweiProvider) GetSecret(name string) (map[string]string, error) {
	if h.client == nil {
		return nil, fmt.Errorf("huawei provider not initialized — Init() was not called")
	}

	req := &model.ShowSecretVersionRequest{
		SecretName: name,
		VersionId:  "latest",
	}

	resp, err := h.client.ShowSecretVersion(req)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch secret '%s' from Huawei CSMS: %w\n"+
				"  Fix: Verify the secret name, region, and IAM credentials",
			name, err,
		)
	}

	if resp.Version == nil || resp.Version.SecretString == nil {
		return nil, fmt.Errorf("huawei CSMS returned an empty secret for %q", name)
	}

	// Try JSON decode first; fall back to {"value": "<raw-string>"} for plain strings.
	var data map[string]string
	if err := json.Unmarshal([]byte(*resp.Version.SecretString), &data); err != nil {
		return map[string]string{"value": *resp.Version.SecretString}, nil
	}
	return data, nil
}

func (h *HuaweiProvider) WatchSecret(name string, interval time.Duration) (<-chan api.SecretUpdate, error) {
	ch := make(chan api.SecretUpdate)
	go func() {
		send := func() {
			val, err := h.GetSecret(name)
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
	// DO NOT perform credential fetching or os.Exit calls here — all
	// initialization happens in Init() after the go-plugin handshake.
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("dso-provider-huawei %s\n", version)
		os.Exit(0)
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: provider.Handshake,
		Plugins: map[string]plugin.Plugin{
			"provider": &provider.SecretProviderPlugin{Impl: &HuaweiProvider{}},
		},
	})
}
