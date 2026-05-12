package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// sortedKeys returns the keys of a map sorted alphabetically
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkPath checks if a path exists and returns a status symbol and error
func checkPath(path string) (string, string) {
	if _, err := os.Stat(path); err == nil {
		return "✓", "exists"
	}
	return "❌ ", "not found"
}

// validateChecksum checks if a file matches the expected hash
func validateChecksum(filepath, expectedHash string) error {
	file, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}

	actualHash := fmt.Sprintf("%x", h.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	return nil
}

// isTerminal checks if stdout is a terminal
func isTerminal() bool {
	// Simple check - can be made more sophisticated
	return os.Getenv("TERM") != ""
}

// validateProviders validates a comma-separated list of providers
func validateProviders(providers string) ([]string, error) {
	if providers == "" {
		return []string{"local"}, nil
	}

	validProviders := map[string]bool{
		"local": true,
		"vault": true,
		"aws":   true,
		"azure": true,
	}

	providerList := strings.Split(providers, ",")
	for _, p := range providerList {
		p = strings.TrimSpace(p)
		if !validProviders[p] {
			return nil, fmt.Errorf("invalid provider: %s", p)
		}
	}

	return providerList, nil
}

// resolveProviders resolves the providers to use, with defaults
func resolveProviders(providers string) ([]string, error) {
	if providers == "" {
		return []string{"local"}, nil
	}
	return validateProviders(providers)
}

// Stub command creators for tests
func newSystemSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Setup DSO system",
	}
}

func newSystemDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Doctor command",
	}
}
