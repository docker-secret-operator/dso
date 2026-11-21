package main

import "testing"

// G013: azcore retries 3 times by default; a negative MaxRetries disables it
// so retries are owned solely by the daemon-side wrapper.
func TestSDKRetriesDisabled(t *testing.T) {
	if got := azureClientOptions().Retry.MaxRetries; got >= 0 {
		t.Fatalf("MaxRetries = %d, want negative (disabled)", got)
	}
}
