//go:build !unix

package config

// PermissionAdvisory is a no-op where POSIX permission bits are not meaningful.
func PermissionAdvisory(string) string { return "" }
