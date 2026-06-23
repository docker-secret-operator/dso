package drift

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// SecretVersionScanner compares provider hash (desired state) vs injected hash (actual state).
// Implements the Detector interface.
//
// Desired  = SecretCache hash (what the provider currently holds).
// Actual   = InjectionRecord hash (what was last successfully injected into a container).
// Drift    = Desired ≠ Actual.
type SecretVersionScanner struct {
	cache          *agent.SecretCache
	cfg            *config.Config
	injectionStore InjectionStore

	mu            sync.Mutex
	lastScanState map[string]string // cacheKey → provider hash at last scan (incremental)
	lastScanTime  time.Time
}

// NewSecretVersionScanner creates the P4 version-drift detector.
func NewSecretVersionScanner(cache *agent.SecretCache, cfg *config.Config, injStore InjectionStore) *SecretVersionScanner {
	return &SecretVersionScanner{
		cache:          cache,
		cfg:            cfg,
		injectionStore: injStore,
		lastScanState:  make(map[string]string),
	}
}

func (s *SecretVersionScanner) ID() string      { return "detector_secret_version" }
func (s *SecretVersionScanner) Name() string    { return "Secret Version Scanner" }
func (s *SecretVersionScanner) Type() DriftType { return DriftVersionMismatch }

// Detect runs the version comparison for each configured secret.
// Incremental: secrets whose provider hash is unchanged since the last scan are skipped
// (their existing open findings are preserved by the engine).
func (s *SecretVersionScanner) Detect(ctx interface{}) ([]DriftFinding, error) {
	c, ok := ctx.(context.Context)
	if !ok {
		c = context.Background()
	}

	if s.cfg == nil || s.cache == nil {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var findings []DriftFinding

	for _, secret := range s.cfg.Secrets {
		name := secret.Name
		provider := secret.Provider
		if provider == "" {
			// Fall back to first configured provider
			for k := range s.cfg.Providers {
				provider = k
				break
			}
		}
		cacheKey := fmt.Sprintf("%s:%s", provider, name)

		providerHash, inCache := s.cache.GetHash(cacheKey)

		// Incremental: skip if provider hash is unchanged since last scan.
		// The engine retains existing open findings for this resource.
		if inCache && s.lastScanState[cacheKey] == providerHash {
			continue
		}

		record, err := s.injectionStore.GetRecord(c, name)
		if err != nil {
			continue
		}

		if !inCache {
			// Provider unreachable or secret never fetched — cannot compare versions.
			findings = append(findings, s.makeFinding(
				DriftMissingSecret,
				SeverityCritical,
				name, provider, "", "", "",
				fmt.Sprintf("Secret %q is not available in the provider cache — may never have been fetched", name),
			))
		} else if record == nil {
			// Provider has the secret, but we have no record of ever injecting it.
			findings = append(findings, s.makeFinding(
				DriftMissingSecret,
				SeverityCritical,
				name, provider, providerHash, "", "",
				fmt.Sprintf("Secret %q has no injection record — container may be running without it", name),
			))
		} else if record.ProviderHash != providerHash {
			// Provider was updated after the last injection — container is stale.
			findings = append(findings, s.makeFinding(
				DriftStaleSecret,
				SeverityHigh,
				name, provider, providerHash, record.ProviderHash, "",
				fmt.Sprintf("Secret %q provider value changed; container injection is stale (injected %s ago)",
					name, time.Since(record.InjectedAt).Round(time.Minute)),
			))
		}
		// If hashes match and no rotation interval is known, the secret is current — no finding.

		// Update incremental state
		s.lastScanState[cacheKey] = providerHash
	}

	s.lastScanTime = time.Now()
	return findings, nil
}

// makeFinding builds a DriftFinding with a deterministic ID.
// Deterministic IDs prevent duplicate findings for the same condition across scans.
func (s *SecretVersionScanner) makeFinding(
	driftType DriftType,
	severity DriftSeverity,
	secretName, provider, expectedVer, actualVer, container string,
	description string,
) DriftFinding {
	id := fmt.Sprintf("drift_%s_%s", strings.ReplaceAll(string(driftType), "_", ""), secretName)
	return DriftFinding{
		ID:          id,
		Type:        driftType,
		Severity:    severity,
		Status:      StatusDetected,
		Resource:    secretName,
		Description: description,
		DetectedAt:  time.Now(),
		Metadata: map[string]interface{}{
			"secret_name":      secretName,
			"provider":         provider,
			"expected_version": expectedVer,
			"actual_version":   actualVer,
			"container":        container,
		},
	}
}
