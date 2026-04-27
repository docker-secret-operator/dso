package main

import (
	"fmt"
	"os"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

const providerName = "huawei"

func main() {
	// --version support: used by `dso system doctor` to display plugin version.
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("dso-provider-%s (stub) %s\n", providerName, version)
		os.Exit(0)
	}

	// All other invocations: fail explicitly and loudly.
	fmt.Fprintf(os.Stderr, "Error: DSO provider '%s' is not yet implemented.\n", providerName)
	fmt.Fprintf(os.Stderr, "       Full Huawei Cloud DEW support is planned for a future release.\n")
	fmt.Fprintf(os.Stderr, "       See: https://github.com/docker-secret-operator/dso/issues\n")
	os.Exit(1)
}
