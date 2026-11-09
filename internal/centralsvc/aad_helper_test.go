package centralsvc

import "testing"

// mustAAD returns a function that unwraps the (aad, error) pair returned by
// the crypto AAD builders: mustAAD(t)(crypto.DEKAAD(...)).
func mustAAD(t testing.TB) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatalf("building AAD: %v", err)
		}
		return b
	}
}
