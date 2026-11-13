// Command cleanup removes the resources of one integration run (identified by
// DSO_TEST_RUN_ID) from the verified test daemon. It never prunes.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker-secret-operator/dso/test/dockerguard"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := dockerguard.CleanupFromEnv(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "cleanup refused or failed:", err)
		os.Exit(1)
	}
	fmt.Println("run resources removed")
}
