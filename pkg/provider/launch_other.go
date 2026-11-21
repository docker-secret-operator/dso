//go:build !linux

package provider

import "path/filepath"

// prepareVerifiedLaunch on non-Linux platforms falls back to the path-based
// verify-then-exec sequence: there is no memfd/sealing primitive, so a window
// remains between hashing and exec that is closed only by the plugin
// directory being writable by trusted users alone (documented residual risk).
func prepareVerifiedLaunch(manifestPath, pluginPath string) (*launchTarget, error) {
	if err := checkDirChainTrust(filepath.Dir(pluginPath)); err != nil {
		return nil, err
	}
	if err := verifyBinaryHash(manifestPath, pluginPath); err != nil {
		return nil, err
	}
	return &launchTarget{path: pluginPath, cleanup: func() {}}, nil
}
