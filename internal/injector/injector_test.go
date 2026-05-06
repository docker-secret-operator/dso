package injector

import (
	"net"
	"net/rpc"
	"os"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

type MockAgent struct{}

func (m *MockAgent) GetSecret(req *api.AgentRequest, resp *api.AgentResponse) error {
	if req.Secret == "error_secret" {
		resp.Error = "simulated agent error"
		return nil
	}
	if req.Secret == "valid_secret" {
		resp.Data = map[string]string{"key": "value"}
		return nil
	}
	resp.Error = "not found"
	return nil
}

func (m *MockAgent) GetEvents(req *api.AgentRequest, resp *api.AgentResponse) error {
	resp.Data = map[string]string{"event": "1"}
	return nil
}

func startMockServer(t *testing.T) string {
	f, _ := os.CreateTemp("/tmp", "dso-test-*.sock")
	socketPath := f.Name()
	f.Close()
	os.Remove(socketPath)

	server := rpc.NewServer()
	err := server.RegisterName("Agent", &MockAgent{})
	if err != nil {
		t.Fatal(err)
	}
	
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go server.ServeConn(conn)
		}
	}()
	
	t.Cleanup(func() {
		listener.Close()
	})
	
	return socketPath
}

func TestAgentClient(t *testing.T) {
	socketPath := startMockServer(t)
	client, err := NewAgentClient(socketPath)
	if err != nil {
		t.Fatal(err)
	}

	// Test Valid Secret
	data, err := client.FetchSecret("prov", nil, "valid_secret")
	if err != nil {
		t.Fatal(err)
	}
	if data["key"] != "value" {
		t.Fatal("unexpected data")
	}

	// Test Error Secret
	_, err = client.FetchSecret("prov", nil, "error_secret")
	if err == nil {
		t.Fatal("expected error")
	}

	// Test GetEvents
	resp, err := client.GetEvents()
	if err != nil {
		t.Fatal(err)
	}
	if resp.Data["event"] != "1" {
		t.Fatal("unexpected event data")
	}
}

func TestAgentClient_FetchAllEnvs(t *testing.T) {
	socketPath := startMockServer(t)
	client, _ := NewAgentClient(socketPath)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"default": {Type: "vault"},
		},
		Secrets: []config.SecretMapping{
			{
				Name: "valid_secret",
				Inject: config.InjectionConfig{Type: "env"},
				Mappings: map[string]string{"key": "MY_ENV"},
			},
		},
	}

	envs, err := client.FetchAllEnvs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if envs["MY_ENV"] != "value" {
		t.Fatal("unexpected env map")
	}
}

func TestAgentClient_FetchAllEnvs_Failures(t *testing.T) {
	socketPath := startMockServer(t)
	client, _ := NewAgentClient(socketPath)

	// Missing provider
	cfg := &config.Config{
		Secrets: []config.SecretMapping{
			{
				Name: "valid_secret", Provider: "missing",
			},
		},
	}
	_, err := client.FetchAllEnvs(cfg)
	if err == nil {
		t.Fatal("expected error")
	}

	// Missing Key
	cfg2 := &config.Config{
		Providers: map[string]config.ProviderConfig{"p": {}},
		Secrets: []config.SecretMapping{
			{
				Name: "valid_secret", Provider: "p",
				Mappings: map[string]string{"missing_key": "ENV"},
			},
		},
	}
	_, err = client.FetchAllEnvs(cfg2)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewAgentClient_ConnectionError(t *testing.T) {
	_, err := NewAgentClient("/invalid/path")
	if err == nil {
		t.Fatal("expected connection error")
	}
}
