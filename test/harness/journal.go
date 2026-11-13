package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RotationRecord mirrors the persisted agent.RotationState (rotations.json) for
// the fields the harness reasons about. A unit test marshals the real type and
// parses it here, so drift in the JSON tags fails the build of the suite.
type RotationRecord struct {
	ProviderName          string    `json:"provider_name"`
	SecretName            string    `json:"secret_name"`
	OriginalContainerIDs  []string  `json:"original_container_ids,omitempty"`
	OriginalContainerID   string    `json:"original_container_id"`
	NewContainerID        string    `json:"new_container_id"`
	RotationID            string    `json:"rotation_id,omitempty"`
	Phase                 string    `json:"phase,omitempty"`
	ServiceID             string    `json:"service_id,omitempty"`
	OwnerID               string    `json:"owner_id,omitempty"`
	Strategy              string    `json:"strategy,omitempty"`
	CandidateContainerIDs []string  `json:"candidate_container_ids,omitempty"`
	Transition            string    `json:"transition,omitempty"`
	PendingIntent         string    `json:"pending_intent,omitempty"`
	ExpectedProxyTarget   string    `json:"expected_proxy_target,omitempty"`
	ObservedProxyTarget   string    `json:"observed_proxy_target,omitempty"`
	Committed             bool      `json:"committed,omitempty"`
	CleanupStatus         string    `json:"cleanup_status,omitempty"`
	Seq                   int       `json:"seq,omitempty"`
	OperationState        string    `json:"operation_state,omitempty"`
	ManualRecovery        bool      `json:"manual_recovery,omitempty"`
	Detail                string    `json:"detail,omitempty"`
	Status                string    `json:"status"`
	StartTime             time.Time `json:"start_time"`
	LastUpdate            time.Time `json:"last_update"`
	ErrorMessage          string    `json:"error_message,omitempty"`
}

// Originals returns the original container IDs (structured field, else the legacy
// comma-joined string).
func (r RotationRecord) Originals() []string {
	if len(r.OriginalContainerIDs) > 0 {
		return append([]string(nil), r.OriginalContainerIDs...)
	}
	var out []string
	for _, p := range strings.Split(r.OriginalContainerID, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseJournal parses the contents of rotations.json.
func ParseJournal(b []byte) ([]RotationRecord, error) {
	m := map[string]*RotationRecord{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]RotationRecord, 0, len(m))
	for _, k := range keys {
		if m[k] != nil {
			out = append(out, *m[k])
		}
	}
	return out, nil
}

// LoadJournal reads <stateDir>/rotations.json. A missing file is an empty journal.
func LoadJournal(stateDir string) ([]RotationRecord, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "rotations.json")) // #nosec G304 -- harness-owned dir
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseJournal(b)
}

// CorruptJournal damages rotations.json beyond JSON parsing (truncated mid-object)
// and returns the original bytes, so a test can compare what recovery preserved.
func CorruptJournal(stateDir string) ([]byte, error) {
	// Root-scoped access: the journal name cannot escape stateDir via a symlink.
	root, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile("rotations.json")
	if err != nil {
		return nil, err
	}
	if len(b) < 20 {
		return nil, fmt.Errorf("journal too small to corrupt meaningfully (%d bytes)", len(b))
	}
	return b, root.WriteFile("rotations.json", b[:len(b)/2], 0o600)
}

// QuarantinedJournals lists the preserved unreadable journal files.
func QuarantinedJournals(stateDir string) []string {
	m, _ := filepath.Glob(filepath.Join(stateDir, "rotations.json.corrupt-*"))
	sort.Strings(m)
	return m
}

// Digest is the comparable essence of a journal: everything recovery decides, and
// nothing time-dependent. Two recovery passes over the same world must produce
// equal digests.
func Digest(recs []RotationRecord) string {
	lines := make([]string, 0, len(recs))
	for _, r := range recs {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%s|committed=%v|pending=%s|op=%s|manual=%v|orig=%s|cand=%s",
			r.RotationID, r.Strategy, r.Status, r.Transition, r.Committed, r.PendingIntent, r.OperationState, r.ManualRecovery,
			strings.Join(r.Originals(), ","), strings.Join(r.CandidateContainerIDs, ",")))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
