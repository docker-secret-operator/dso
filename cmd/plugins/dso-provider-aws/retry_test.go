package main

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
)

// G013: retries are owned by the daemon-side wrapper; the SDK must make a
// single attempt or attempts multiply (SDK x wrapper).
func TestSDKRetriesDisabled(t *testing.T) {
	var lo config.LoadOptions
	if err := sdkRetryOption()(&lo); err != nil {
		t.Fatal(err)
	}
	if lo.RetryMaxAttempts != 1 {
		t.Fatalf("RetryMaxAttempts = %d, want 1", lo.RetryMaxAttempts)
	}
}
