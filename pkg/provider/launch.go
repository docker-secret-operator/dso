package provider

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// launchTarget is what LoadProvider hands to exec: a path plus any inherited
// descriptors the path depends on (the sealed memfd on Linux).
type launchTarget struct {
	path       string
	extraFiles []*os.File
	cleanup    func()
}

// afterVerifyHook is a test seam: called after verification completes and
// before the launch target is returned, so tests can tamper with the on-disk
// file at the worst possible moment. Always nil in production.
var afterVerifyHook func()

var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// manifestHashFor extracts the expected SHA-256 for pluginName from manifest
// content ("name=sha256hex" per line, '#' comments). The entry must be unique
// and a well-formed lowercase 64-hex digest; malformed lines for other names
// are ignored, a malformed entry for this plugin is an error.
func manifestHashFor(r io.Reader, manifestPath, pluginName string) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024), 1024)
	expected, seen := "", false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) != pluginName {
			continue
		}
		if seen {
			return "", fmt.Errorf("duplicate manifest entry for plugin %q", pluginName)
		}
		expected, seen = strings.TrimSpace(line[idx+1:]), true
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading hash manifest: %w", err)
	}
	if !seen {
		return "", fmt.Errorf("plugin %s not found in hash manifest %s", pluginName, manifestPath)
	}
	if !sha256HexRe.MatchString(expected) {
		return "", fmt.Errorf("malformed manifest entry for plugin %q: want 64 lowercase hex characters", pluginName)
	}
	return expected, nil
}

// prepareLaunch is the policy gate used by LoadProvider. With verification
// skipped (explicit operator opt-out) the path is launched as-is. Otherwise
// it returns a target whose executed bytes were verified (see
// prepareVerifiedLaunch for the per-platform guarantee).
func prepareLaunch(pluginDir, providerName, pluginName, pluginPath string) (*launchTarget, error) {
	if skipHashVerification() {
		fmt.Printf("[DSO] WARNING: hash verification for plugin %s SKIPPED via DSO_PLUGIN_SKIP_HASH_VERIFY (path: %s)\n", pluginName, pluginPath)
		return &launchTarget{path: pluginPath, cleanup: func() {}}, nil
	}
	manifestPath := os.Getenv("DSO_PLUGIN_HASH_MANIFEST")
	if manifestPath == "" {
		manifestPath = filepath.Join(filepath.Clean(pluginDir), defaultManifestFileName)
	}
	t, err := prepareVerifiedLaunch(manifestPath, pluginPath)
	if err != nil {
		return nil, fmt.Errorf(
			"plugin %s failed hash verification: %w\n"+
				"  Manifest: %s\n"+
				"  Fix: sudo docker dso system setup --providers %s (regenerates the manifest)\n"+
				"  Escape hatch (not recommended): DSO_PLUGIN_SKIP_HASH_VERIFY=1",
			pluginName, err, manifestPath, providerName,
		)
	}
	return t, nil
}
