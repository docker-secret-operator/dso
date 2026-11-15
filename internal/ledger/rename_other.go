//go:build !linux && !darwin

package ledger

// renameNoReplace has no atomic no-overwrite rename on this platform, so
// preservation fails closed (owner decision D-S2-4).
func renameNoReplace(string, string) error { return ErrUnsupportedPlatform }
