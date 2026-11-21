//go:build !dsofault

// Package faultinject is the TEST-ONLY crash hook used by the real-process
// crash-recovery harness (test/docker, docs/testing/REAL-PROCESS-CRASH-TESTS.md).
//
// This is the production file. Without the `dsofault` build tag the hook does not
// exist: Hit never fires, Param never overrides anything, and this binary reads no
// fault-injection environment variable (the variable names live ONLY in the
// dsofault-tagged file). There is no network API and no runtime switch that can
// turn the hook on in a binary built without the tag.
package faultinject

// Enabled reports whether this binary was built with the dsofault tag. It is a
// constant so the compiler removes every guarded call site.
const Enabled = false

// Hit is called at a named transition point. In this build it never fires.
func Hit(point string) bool { return false }

// Param returns a test-only override for a named setting. In this build it never
// overrides anything, so production directories and settings are always used.
func Param(name string) (string, bool) { return "", false }
