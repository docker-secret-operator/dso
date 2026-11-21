//go:build !unix

package provider

import "os"

// Non-unix platforms have no uid/mode/link-count model to enforce; the path
// based allowlist checks remain the only gate there.
func openNoFollow(path string) (*os.File, error) { return os.Open(path) } // #nosec G304

func checkFileTrust(f *os.File, what string, singleLink bool) (os.FileInfo, error) {
	return f.Stat()
}

func checkDirChainTrust(dir string) error { return nil }
