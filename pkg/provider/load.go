package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/backend/env"
	"github.com/docker-secret-operator/dso/pkg/backend/file"
	"github.com/hashicorp/go-plugin"
)

// validatePluginPath performs security checks on plugin path to prevent command injection (CWE-78)
func validatePluginPath(path string) error {
	// 1. Check if path is absolute
	if !filepath.IsAbs(path) {
		return fmt.Errorf("plugin path must be absolute: %s", path)
	}

	// 2. Verify path is within allowed directory
	allowedDirs := []string{
		"/var/lib/dso/plugins",
		"/usr/local/lib/dso/plugins",
		"/etc/dso/plugins",
	}

	isAllowed := false
	for _, dir := range allowedDirs {
		rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return fmt.Errorf("plugin must be in allowed directory: %s", path)
	}

	// 3. Check file exists and is not a symlink
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("plugin not accessible: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("plugin cannot be a symlink")
	}

	// 4. Verify file is executable
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("plugin must be executable")
	}

	return nil
}

// isValidProviderName ensures the provider name is strictly alphanumeric to prevent path traversal
func isValidProviderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// sanitizeEnv returns a safe environment for plugin execution
func sanitizeEnv() []string {
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
	}
}

// LoadProvider dynamically executes the provider binary and dispenses the RPC client
func LoadProvider(providerName string, providerConfig map[string]string) (api.SecretProvider, *plugin.Client, error) {
	// 1. Check for native local backends first
	switch providerName {
	case "file":
		prov := &file.FileProvider{}
		if providerConfig == nil {
			providerConfig = make(map[string]string)
		}
		if err := prov.Init(providerConfig); err != nil {
			return nil, nil, fmt.Errorf("local file provider failed to initialize: %w", err)
		}
		return prov, nil, nil
	case "env":
		prov := &env.EnvProvider{}
		return prov, nil, nil
	}

	// 2. Load external plugins
	pluginDir := os.Getenv("DSO_PLUGIN_DIR")
	if pluginDir == "" {
		pluginDir = "/usr/local/lib/dso/plugins"
	}
	if !isValidProviderName(providerName) {
		return nil, nil, fmt.Errorf("invalid provider name: %s", providerName)
	}

	pluginName := fmt.Sprintf("dso-provider-%s", providerName)
	pluginPath := filepath.Join(filepath.Clean(pluginDir), pluginName)

	if err := validatePluginPath(pluginPath); err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "not accessible") {
			return nil, nil, fmt.Errorf(
				"provider plugin '%s' is not installed.\n"+
					"  Expected: %s\n"+
					"  Fix: sudo docker dso system setup --providers %s\n"+
					"  Run: docker dso system doctor",
				providerName, pluginPath, providerName,
			)
		}
		return nil, nil, fmt.Errorf("security validation for plugin %s failed: %w", pluginName, err)
	}

	// G702/G204: pluginPath is strictly validated above to be in allowed system directories.
	// This is a plugin-based architecture where the command must be dynamic.
	fmt.Printf("[DSO] Using %s provider plugin: %s\n", strings.ToUpper(providerName), pluginPath)
	cmd := exec.Command(pluginPath) // #nosec G702 G204
	cmd.Env = sanitizeEnv()

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             cmd,
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, client, fmt.Errorf("failed to start provider plugin %q: binary could not be executed or failed to respond: %w (path: %s)", providerName, err, pluginPath)
	}

	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, client, fmt.Errorf("failed to load provider %q: plugin is corrupt or incompatible: %w (path: %s)", providerName, err, pluginPath)
	}

	prov := raw.(api.SecretProvider)

	// Inject the dynamic YAML configuration map
	if providerConfig == nil {
		providerConfig = make(map[string]string)
	}
	if err := prov.Init(providerConfig); err != nil {
		client.Kill()
		return nil, client, fmt.Errorf("provider %q failed to initialize: invalid configuration, bad credentials, or network timeout: %w (path: %s)", providerName, err, pluginPath)
	}

	return prov, client, nil
}
