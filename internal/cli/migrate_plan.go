package cli

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compose"
	"gopkg.in/yaml.v3"
)

// secretNameHints are substrings that mark an env var name as a migration
// *candidate* -- an assist for the user to review, never a decision made
// on the user's behalf. Matching is case-insensitive and by substring, so
// e.g. "DB_PASSWORD", "JWT_SECRET", "API_TOKEN" all match.
var secretNameHints = []string{
	"PASSWORD", "SECRET", "TOKEN", "API_KEY", "APIKEY", "PRIVATE_KEY",
	"CREDENTIAL", "AUTH", "ACCESS_KEY", "CLIENT_SECRET", "SIGNING_KEY",
}

// looksLikeSecretName reports whether name matches one of secretNameHints.
// This is intentionally a *hint*, not an assertion -- see migrate's
// selection flow, which always shows the user both buckets before
// anything is imported.
func looksLikeSecretName(name string) bool {
	upper := strings.ToUpper(name)
	for _, hint := range secretNameHints {
		if strings.Contains(upper, hint) {
			return true
		}
	}
	return false
}

// interpolationPattern matches a compose value that is *exactly* one
// ${VAR}, ${VAR:-default}, or ${VAR:?msg} shell-style interpolation
// expression referencing a single variable name. A value that mixes
// interpolation with literal text (e.g. "prefix-${VAR}") intentionally
// does not match -- migrate proposes no automatic transform for it rather
// than guessing how to split it.
var interpolationPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(:?[-?][^}]*)?\}$`)

func extractInterpolatedVar(value string) (string, bool) {
	m := interpolationPattern.FindStringSubmatch(value)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// MigrationCandidate is one .env key under consideration for migration.
type MigrationCandidate struct {
	Key           string
	LooksSecret   bool
	Selected      bool
	ExistsInVault bool
	VaultDiffers  bool // only meaningful when ExistsInVault is true
}

// ComposeChange is one proposed environment-value substitution.
type ComposeChange struct {
	Service  string
	EnvKey   string
	OldValue string // e.g. "${DB_PASSWORD}" -- interpolation syntax only, never a resolved secret
	NewURI   string // e.g. "dso://myproject/DB_PASSWORD"
}

// ManualReviewItem flags a service the migration could not safely
// transform automatically.
type ManualReviewItem struct {
	Service string
	Reason  string
}

// MigrationPlan is the complete, side-effect-free result of analyzing a
// .env + Compose project. Building a plan never touches the filesystem
// (beyond reading the two input files) or the vault (beyond read-only
// existence lookups via the injected VaultLookup). Nothing here is applied
// until the caller explicitly calls Apply.
type MigrationPlan struct {
	EnvPath     string
	ComposePath string
	ProjectName string

	AllVars         []string
	Candidates      []MigrationCandidate
	NonSecretVars   []string
	AlreadyMigrated []string
	DuplicateKeys   []string

	ComposeChanges []ComposeChange
	ManualReview   []ManualReviewItem
	Warnings       []string

	// envValues is intentionally unexported: it holds plaintext secret
	// values in memory (required to actually import them later) but must
	// never be serialized, logged, or included in any rendered preview.
	envValues map[string]string
}

// vaultLookupFunc reports whether a secret already exists at project/key
// and, if so, whether its stored value differs from candidateValue. It
// never returns the stored value itself -- planning code has no need to
// see it, only to compare.
type vaultLookupFunc func(project, key, candidateValue string) (exists bool, differs bool, err error)

// planMigration builds a MigrationPlan from an .env file and a Compose
// file. It performs no writes. vaultLookup may be nil, in which case
// conflict detection is skipped (used by dry-run previews that don't want
// to require an initialized vault just to show a plan).
func planMigration(envPath, composePath, projectName string, vaultLookup vaultLookupFunc) (*MigrationPlan, error) {
	plan := &MigrationPlan{
		EnvPath:     envPath,
		ComposePath: composePath,
		ProjectName: projectName,
		envValues:   make(map[string]string),
	}

	envFile, err := os.Open(envPath) // #nosec G304 -- envPath is caller-validated via config.IsSafePath before planMigration is called
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", envPath, err)
	}
	defer func() { _ = envFile.Close() }()

	parsedEnv, err := parseDotEnv(envFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", envPath, err)
	}
	plan.envValues = parsedEnv.Values
	plan.DuplicateKeys = parsedEnv.DuplicateKeys
	for _, skip := range parsedEnv.SkippedLines {
		plan.Warnings = append(plan.Warnings, ".env "+skip)
	}

	for k := range parsedEnv.Values {
		plan.AllVars = append(plan.AllVars, k)
	}
	sort.Strings(plan.AllVars)

	composeContent, err := os.ReadFile(composePath) // #nosec G304 -- composePath is caller-validated before planMigration is called
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", composePath, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(composeContent, &root); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", composePath, err)
	}

	alreadyMigrated := map[string]bool{}

	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	servicesNode := compose.GetMapValue(doc, "services")
	if servicesNode != nil && servicesNode.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(servicesNode.Content); i += 2 {
			serviceName := servicesNode.Content[i].Value
			serviceBody := servicesNode.Content[i+1]
			if serviceBody == nil || serviceBody.Kind != yaml.MappingNode {
				continue
			}

			if compose.GetMapValue(serviceBody, "env_file") != nil {
				plan.ManualReview = append(plan.ManualReview, ManualReviewItem{
					Service: serviceName,
					Reason:  "uses env_file: -- DSO cannot safely infer which of its keys are secrets; migrate its values manually with 'docker dso secret set' or 'docker dso env import'",
				})
			}

			envNode := compose.GetMapValue(serviceBody, "environment")
			if envNode == nil {
				continue
			}

			for envKey, rawValue := range envEntries(envNode) {
				switch {
				case strings.HasPrefix(rawValue, "dso://"), strings.HasPrefix(rawValue, "dsofile://"):
					alreadyMigrated[envKey] = true
				default:
					if varName, ok := extractInterpolatedVar(rawValue); ok {
						if _, inEnv := parsedEnv.Values[varName]; inEnv {
							plan.ComposeChanges = append(plan.ComposeChanges, ComposeChange{
								Service:  serviceName,
								EnvKey:   envKey,
								OldValue: rawValue,
								NewURI:   fmt.Sprintf("dso://%s/%s", projectName, varName),
							})
						}
					}
				}
			}
		}
	}

	for k := range alreadyMigrated {
		plan.AlreadyMigrated = append(plan.AlreadyMigrated, k)
	}
	sort.Strings(plan.AlreadyMigrated)

	for _, k := range plan.AllVars {
		if alreadyMigrated[k] {
			continue
		}
		candidate := MigrationCandidate{
			Key:         k,
			LooksSecret: looksLikeSecretName(k),
			Selected:    looksLikeSecretName(k),
		}
		if vaultLookup != nil && candidate.Selected {
			exists, differs, err := vaultLookup(projectName, k, parsedEnv.Values[k])
			if err != nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("could not check existing vault entry for %s: %v", k, err))
			} else {
				candidate.ExistsInVault = exists
				candidate.VaultDiffers = differs
			}
		}
		if candidate.LooksSecret {
			plan.Candidates = append(plan.Candidates, candidate)
		} else {
			plan.NonSecretVars = append(plan.NonSecretVars, k)
		}
	}

	sort.Slice(plan.ComposeChanges, func(i, j int) bool {
		if plan.ComposeChanges[i].Service != plan.ComposeChanges[j].Service {
			return plan.ComposeChanges[i].Service < plan.ComposeChanges[j].Service
		}
		return plan.ComposeChanges[i].EnvKey < plan.ComposeChanges[j].EnvKey
	})
	sort.Slice(plan.ManualReview, func(i, j int) bool { return plan.ManualReview[i].Service < plan.ManualReview[j].Service })

	return plan, nil
}

// envEntries extracts key/raw-value pairs from an `environment:` node in
// either its mapping form (KEY: value) or list form (KEY=value).
func envEntries(envNode *yaml.Node) map[string]string {
	out := map[string]string{}
	switch envNode.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(envNode.Content); i += 2 {
			key := envNode.Content[i].Value
			val := envNode.Content[i+1]
			if val != nil && val.Kind == yaml.ScalarNode {
				out[key] = val.Value
			}
		}
	case yaml.SequenceNode:
		for _, item := range envNode.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			parts := strings.SplitN(item.Value, "=", 2)
			if len(parts) == 2 {
				out[parts[0]] = parts[1]
			}
		}
	}
	return out
}

// SelectedComposeChanges returns only the ComposeChanges whose underlying
// candidate is currently selected -- i.e. what Apply will actually write.
func (p *MigrationPlan) SelectedComposeChanges() []ComposeChange {
	selected := map[string]bool{}
	for _, c := range p.Candidates {
		if c.Selected {
			selected[c.Key] = true
		}
	}
	var out []ComposeChange
	for _, change := range p.ComposeChanges {
		if varName, ok := extractInterpolatedVar(change.OldValue); ok && selected[varName] {
			out = append(out, change)
		}
	}
	return out
}
