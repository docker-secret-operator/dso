package injector

import (
	"fmt"
	"net/rpc"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

type AgentClient struct {
	client *rpc.Client
}

func NewAgentClient(socketPath string) (*AgentClient, error) {
	client, err := rpc.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the DSO agent socket at %s: %w", socketPath, err)
	}
	return &AgentClient{client: client}, nil
}

func (ac *AgentClient) FetchSecret(providerName string, config map[string]string, secretName string) (map[string]string, error) {
	req := &api.AgentRequest{
		Provider: providerName,
		Config:   config,
		Secret:   secretName,
	}
	var resp api.AgentResponse
	if err := ac.client.Call("Agent.GetSecret", req, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("agent error: %s", resp.Error)
	}
	return resp.Data, nil
}

// FetchAllEnvs aggregates all secrets mapped to environments for a given config.
func (ac *AgentClient) FetchAllEnvs(cfg *config.Config) (map[string]string, error) {
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

			data, err := ac.FetchSecret(pName, pCfg.Config, sec.Name)
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
