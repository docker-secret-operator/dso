//go:build !unix

package sqlite

// InstallStagingSignalCleanup is a no-op on platforms without the unix signal
// semantics the central store requires anyway.
func InstallStagingSignalCleanup() (stop func()) { return func() {} }
