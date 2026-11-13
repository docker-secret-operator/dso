// Command faultprobe is a minimal binary that links internal/faultinject, so the
// release-guard tests can build it with and without -tags=dsofault and inspect the
// result for the DSO_FAULT_ strings. It deliberately avoids the rest of the tree.
package main

import (
	"fmt"

	"github.com/docker-secret-operator/dso/internal/faultinject"
)

func main() {
	p, ok := faultinject.Param("PLUGIN_DIR")
	fmt.Println(faultinject.Enabled, faultinject.Hit("probe"), p, ok)
}
