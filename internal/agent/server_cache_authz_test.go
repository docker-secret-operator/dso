package agent

import (
	"testing"

	"github.com/docker-secret-operator/dso/pkg/api"
)

// Synthetic placeholder only -- never real secret material. Deliberately
// different from the on-disk value so a test can tell a cache hit from a
// provider fetch.
const syntheticCachedValue = "synthetic-cached-value"

// TestGetSecret_CacheFastPath_UnauthorizedCallerDenied is the regression test
// for the cache fast-path authorization bypass: a value already in the cache
// (from an earlier authorized call or from the agent's pollers) must not be
// returned to a caller whose request fails the dso.yaml authorization check.
func TestGetSecret_CacheFastPath_UnauthorizedCallerDenied(t *testing.T) {
	cases := []struct {
		name     string
		cacheKey string
		req      *api.AgentRequest
	}{
		{
			// Configured provider, secret not mapped to it.
			name:     "secret not in mappings",
			cacheKey: "files:unlisted",
			req:      &api.AgentRequest{Provider: "files", Secret: "unlisted"},
		},
		{
			// Provider not defined in dso.yaml at all.
			name:     "unconfigured provider",
			cacheKey: "vault:allowed",
			req:      &api.AgentRequest{Provider: "vault", Secret: "allowed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newAuthzTestServer(t)
			s.Cache.Set(tc.cacheKey, map[string]string{"value": syntheticCachedValue})

			resp := &api.AgentResponse{}
			err := s.GetSecret(tc.req, resp)
			assertDenied(t, err, resp)
			if len(resp.Data) != 0 {
				t.Fatalf("denied caller received cached data: %v", resp.Data)
			}
		})
	}
}

// TestGetSecret_CacheFastPath_AuthorizedCallerServedFromCache proves the
// fix does not regress the legitimate path: an authorized caller is still
// answered from the cache (the cached sentinel, not the provider's on-disk
// value, comes back), including when req.Provider is left empty and resolved
// from the secret's mapping.
func TestGetSecret_CacheFastPath_AuthorizedCallerServedFromCache(t *testing.T) {
	for _, provider := range []string{"files", ""} {
		t.Run("provider="+provider, func(t *testing.T) {
			s, _ := newAuthzTestServer(t)
			s.Cache.Set("files:allowed", map[string]string{"value": syntheticCachedValue})

			resp := &api.AgentResponse{}
			if err := s.GetSecret(&api.AgentRequest{Provider: provider, Secret: "allowed"}, resp); err != nil {
				t.Fatalf("authorized request failed: %v (resp.Error=%q)", err, resp.Error)
			}
			if got := resp.Data["value"]; got != syntheticCachedValue {
				t.Fatalf("expected cached value %q, got %q (cache not used)", syntheticCachedValue, got)
			}
		})
	}
}
