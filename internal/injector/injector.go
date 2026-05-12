package injector

import (
	"context"
	"fmt"
	"net/rpc"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

type AgentClient struct {
	client         *rpc.Client
	requestTimeout time.Duration
}

func NewAgentClient(socketPath string) (*AgentClient, error) {
	return NewAgentClientWithTimeout(socketPath, 30*time.Second)
}

func NewAgentClientWithTimeout(socketPath string, timeout time.Duration) (*AgentClient, error) {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DSO agent socket at %s: %w", socketPath, err)
	}
	return &AgentClient{
		client:         client,
		requestTimeout: timeout,
	}, nil
}

func (ac *AgentClient) FetchSecret(providerName string, config map[string]string, secretName string) (map[string]string, error) {
	return ac.FetchSecretWithContext(context.Background(), providerName, config, secretName)
}

func (ac *AgentClient) FetchSecretWithContext(ctx context.Context, providerName string, config map[string]string, secretName string) (map[string]string, error) {
	// Apply timeout if no deadline already set
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ac.requestTimeout)
		defer cancel()
	}

	req := &api.AgentRequest{
		Provider: providerName,
		Config:   config,
		Secret:   secretName,
	}

	if ac.client == nil {
		return nil, fmt.Errorf("agent client not initialized")
	}

	// Check if context is already done before starting RPC
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("secret fetch timeout for provider %s secret %s: %w", providerName, secretName, ctx.Err())
	default:
	}

	respCh := make(chan error, 1)
	var resp api.AgentResponse
	go func() {
		respCh <- ac.client.Call("Agent.GetSecret", req, &resp)
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("secret fetch timeout for provider %s secret %s: %w", providerName, secretName, ctx.Err())
	case err := <-respCh:
		if err != nil {
			return nil, fmt.Errorf("rpc call failed: %w", err)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("agent error: %s", resp.Error)
		}
		return resp.Data, nil
	}
}

// FetchAllEnvs aggregates all secrets mapped to environments for a given config.
func (ac *AgentClient) FetchAllEnvs(cfg *config.Config) (map[string]string, error) {
	return ac.FetchAllEnvsWithContext(context.Background(), cfg)
}

// FetchAllEnvsWithContext aggregates all secrets mapped to environments with context propagation.
func (ac *AgentClient) FetchAllEnvsWithContext(ctx context.Context, cfg *config.Config) (map[string]string, error) {
	envs := make(map[string]string)
	for _, sec := range cfg.Secrets {
		if sec.Inject.Type == "env" || sec.Inject.Type == "" { // default to env
			pName := sec.Provider
			if pName == "" {
				// Default to first provider if none specified
				for k := range cfg.Providers {
					pName = k
					break
				}
			}
			pCfg, ok := cfg.Providers[pName]
			if !ok {
				return nil, fmt.Errorf("provider %s not found for secret %s", pName, sec.Name)
			}

			data, err := ac.FetchSecretWithContext(ctx, pName, pCfg.Config, sec.Name)
			if err != nil {
				return nil, err
			}
			for mapKey, envName := range sec.Mappings {
				if val, ok := data[mapKey]; ok {
					envs[envName] = val
				} else {
					return nil, fmt.Errorf("key %s not found in secret %s", mapKey, sec.Name)
				}
			}
		}
	}
	return envs, nil
}

func (ac *AgentClient) GetEvents() (*api.AgentResponse, error) {
	req := &api.AgentRequest{}
	var resp api.AgentResponse
	if err := ac.client.Call("Agent.GetEvents", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Close closes the RPC connection and releases resources
func (ac *AgentClient) Close() error {
	if ac.client != nil {
		return ac.client.Close()
	}
	return nil
}
