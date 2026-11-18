package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrExistingConfig is returned (wrapped) when bootstrap would replace an existing DSO
// configuration without the explicit ReplaceExistingConfig opt-in.
var ErrExistingConfig = errors.New("a DSO configuration already exists")

// refuseExistingConfig stops bootstrap from silently replacing a configuration that is
// already there. The generated file is a template; written over a working host's dso.yaml it
// destroys provider settings, secret mappings and agent.owner_id (losing the owner id orphans
// every container the agent created). Call it before prompting the operator or changing
// anything. A missing file is fine; replace == true (the explicit opt-in) allows it.
func refuseExistingConfig(configPath string, replace bool) error {
	info, err := os.Lstat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot examine the existing configuration path %s: %w", configPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s exists and is not a regular file; refusing to write over it", configPath)
	}
	if replace {
		return nil
	}
	return fmt.Errorf("%w at %s and bootstrap will not replace it: re-run with --replace-config to replace it "+
		"(the existing file is backed up first), or edit it in place", ErrExistingConfig, configPath)
}

// backupExistingConfig writes a timestamped 0600 copy of the file at configPath next to it
// (<path>.bak-<UTC timestamp>, a numeric suffix if that name is taken) and returns its path.
// It is a no-op returning "" when there is no file. The backup may hold credentials, so it is
// created exclusively with mode 0600 and fsynced; if it cannot be written the caller must not
// replace the file.
func backupExistingConfig(configPath string) (string, error) {
	data, err := os.ReadFile(configPath) // #nosec G304 -- configPath is DSO's own config location
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read the existing configuration for backup: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for n := 0; n < 100; n++ {
		name := fmt.Sprintf("%s.bak-%s", configPath, stamp)
		if n > 0 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- next to DSO's own config
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("create the configuration backup: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("write the configuration backup: %w", err)
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("sync the configuration backup: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("close the configuration backup: %w", err)
		}
		return name, nil
	}
	return "", fmt.Errorf("no free backup name next to %s", configPath)
}
