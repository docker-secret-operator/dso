package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compose"
	"github.com/docker-secret-operator/dso/internal/setup"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"gopkg.in/yaml.v3"
)

// Categories specific to `dso validate`'s deeper, apply-time checks.
// doctorCatProject (from doctor.go) already covers doctor's fast,
// syntax-only project checks; these are validate's own, deeper layer --
// kept as distinct categories so the two commands' output sections never
// collide, even though both render through the same setup.DoctorCheck type.
const (
	validateCatCompose    setup.DoctorCategory = "compose"
	validateCatReferences setup.DoctorCategory = "references"
	validateCatSecrets    setup.DoctorCategory = "secrets"
)

// dsoReference is one resolved dso:// or dsofile:// reference found while
// walking a Compose file, kept only as (service, key, scheme, project,
// path) -- never a secret value.
type dsoReference struct {
	Service string
	EnvKey  string
	Scheme  string // "dso" or "dsofile"
	Project string
	Path    string
	RawURI  string
}

// parseDSOReference mirrors internal/resolver's URI-splitting rule
// (project/path split on the first '/', both trimmed and required
// non-empty, single-segment URIs default to fallbackProject) without
// importing resolver's unexported parser. This is a five-line format
// rule, not a re-implementation of secret resolution -- the same
// reasoning already applied to doctor.go's lighter-weight reference check.
func parseDSOReference(uri, fallbackProject string) (scheme, project, path string, ok bool) {
	var rest string
	switch {
	case strings.HasPrefix(uri, "dsofile://"):
		scheme, rest = "dsofile", strings.TrimPrefix(uri, "dsofile://")
	case strings.HasPrefix(uri, "dso://"):
		scheme, rest = "dso", strings.TrimPrefix(uri, "dso://")
	default:
		return "", "", "", false
	}

	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		project = fallbackProject
		path = parts[0]
	} else {
		project = parts[0]
		path = parts[1]
	}
	project = strings.TrimSpace(project)
	path = strings.TrimSpace(path)
	if project == "" || path == "" {
		return scheme, project, path, false
	}
	return scheme, project, path, true
}

// checkComposeStructure validates that composePath exists, is readable,
// parses as YAML, and has a well-formed `services:` block. It returns the
// parsed root node (nil on failure) so callers can proceed to deeper
// checks without re-parsing.
func checkComposeStructure(composePath string) (*yaml.Node, []setup.DoctorCheck) {
	var checks []setup.DoctorCheck

	content, err := os.ReadFile(composePath) // #nosec G304 -- composePath is caller-validated via config.IsSafePath before this is called
	if err != nil {
		return nil, append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-001", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorCritical,
			Name: "Compose file readable", Description: "The Compose file must exist and be readable",
			Detail: fmt.Sprintf("failed to read %s: %v", composePath, err),
		})
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-002", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "Compose syntax", Description: "The Compose file must be valid YAML",
			Detail:    fmt.Sprintf("YAML parse error: %v", err),
			RootCause: "Compose file contains invalid YAML",
			Recovery:  []string{"Fix the YAML syntax and re-run validate"},
		})
	}
	checks = append(checks, setup.DoctorCheck{
		ID: "DSO-VALIDATE-002", Category: validateCatCompose, Status: setup.DoctorPass,
		Name: "Compose syntax", Description: "The Compose file must be valid YAML", Detail: "valid YAML",
	})

	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	servicesNode := compose.GetMapValue(doc, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return &root, append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-003", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "services: block", Description: "The Compose file must define a services: mapping",
			Detail:    "no valid services: mapping found",
			RootCause: "A Compose file with no services has nothing for DSO to manage",
			Recovery:  []string{"Add a services: block to " + composePath},
		})
	}

	var malformed []string
	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		name := servicesNode.Content[i].Value
		body := servicesNode.Content[i+1]
		if body == nil || body.Kind != yaml.MappingNode {
			malformed = append(malformed, name)
		}
	}
	if len(malformed) > 0 {
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-003", Category: validateCatCompose, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "services: block", Description: "Every service must be a valid mapping",
			Detail:    fmt.Sprintf("malformed service definition(s): %s", strings.Join(malformed, ", ")),
			RootCause: "A service value that isn't a mapping (e.g. null, a scalar, or a list) cannot be deployed",
			Recovery:  []string{"Fix the definition for: " + strings.Join(malformed, ", ")},
		})
	} else {
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-003", Category: validateCatCompose, Status: setup.DoctorPass,
			Name: "services: block", Description: "Every service must be a valid mapping",
			Detail: fmt.Sprintf("%d service(s), all well-formed", (len(servicesNode.Content))/2),
		})
	}

	return &root, checks
}

// collectDSOReferences walks every service's `environment:` block and
// returns every dso:// / dsofile:// reference found, plus validation
// checks for their syntax. fallbackProject is used for single-segment
// URIs (dso://path with no explicit project), matching resolver's rule.
func collectDSOReferences(root *yaml.Node, fallbackProject string) ([]dsoReference, []setup.DoctorCheck) {
	var refs []dsoReference
	var malformed []string

	doc := root
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
			envNode := compose.GetMapValue(serviceBody, "environment")
			if envNode == nil {
				continue
			}
			for envKey, rawValue := range envEntries(envNode) {
				if !strings.HasPrefix(rawValue, "dso://") && !strings.HasPrefix(rawValue, "dsofile://") {
					continue
				}
				scheme, project, path, ok := parseDSOReference(rawValue, fallbackProject)
				if !ok {
					malformed = append(malformed, fmt.Sprintf("%s.%s", serviceName, envKey))
					continue
				}
				refs = append(refs, dsoReference{
					Service: serviceName, EnvKey: envKey,
					Scheme: scheme, Project: project, Path: path, RawURI: rawValue,
				})
			}
		}
	}

	var checks []setup.DoctorCheck
	if len(malformed) > 0 {
		sort.Strings(malformed)
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-004", Category: validateCatReferences, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "DSO reference syntax", Description: "Every dso:// / dsofile:// reference must specify a non-empty secret path",
			Detail:    fmt.Sprintf("%d malformed reference(s): %s", len(malformed), strings.Join(malformed, ", ")),
			RootCause: "A dso:// or dsofile:// URI is missing its secret path",
			Recovery:  []string{"Fix the malformed reference(s) listed above"},
		})
	} else if len(refs) > 0 {
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-004", Category: validateCatReferences, Status: setup.DoctorPass,
			Name: "DSO reference syntax", Description: "Every dso:// / dsofile:// reference must specify a non-empty secret path",
			Detail: fmt.Sprintf("%d reference(s), all well-formed", len(refs)),
		})
	} else {
		checks = append(checks, setup.DoctorCheck{
			ID: "DSO-VALIDATE-004", Category: validateCatReferences, Status: setup.DoctorInfo,
			Name: "DSO reference syntax", Description: "Every dso:// / dsofile:// reference must specify a non-empty secret path",
			Detail: "no DSO references found in this Compose file",
		})
	}

	checks = append(checks, checkReferenceConsistency(refs))
	return refs, checks
}

// checkReferenceConsistency warns when the same environment-variable name
// is mapped to more than one distinct secret path across services -- a
// common copy/paste mistake, not necessarily an error (e.g. one service
// intentionally using a per-tenant path), so this is a WARN, not a FAIL.
func checkReferenceConsistency(refs []dsoReference) setup.DoctorCheck {
	byKey := map[string]map[string]bool{}
	for _, r := range refs {
		id := r.Project + "/" + r.Path
		if byKey[r.EnvKey] == nil {
			byKey[r.EnvKey] = map[string]bool{}
		}
		byKey[r.EnvKey][id] = true
	}

	var inconsistent []string
	for key, targets := range byKey {
		if len(targets) > 1 {
			inconsistent = append(inconsistent, key)
		}
	}
	sort.Strings(inconsistent)

	if len(inconsistent) > 0 {
		return setup.DoctorCheck{
			ID: "DSO-VALIDATE-005", Category: validateCatReferences, Status: setup.DoctorWarn,
			Name: "Reference consistency", Description: "The same environment-variable name mapped to different secrets across services may be unintentional",
			Detail:    fmt.Sprintf("inconsistently mapped key(s): %s", strings.Join(inconsistent, ", ")),
			RootCause: "Two or more services reference the same key name but point at different secret paths",
			Recovery:  []string{"Confirm this is intentional, or align the references to the same secret path"},
		}
	}
	return setup.DoctorCheck{
		ID: "DSO-VALIDATE-005", Category: validateCatReferences, Status: setup.DoctorPass,
		Name: "Reference consistency", Description: "The same environment-variable name mapped to different secrets across services may be unintentional",
		Detail: "no inconsistent key mappings found",
	}
}

// checkSecretExistence performs read-only, metadata/existence-only lookups
// for each distinct (project, path) reference. It never retrieves or
// prints a secret's plaintext value -- vault.Get returns a value, but only
// the error (found/not-found) is ever inspected here.
//
// Cloud-mode provider secrets are explicitly out of scope: pkg/api's
// SecretProvider interface exposes only GetSecret (which returns the
// plaintext), with no existence-only method. Calling it just to validate
// existence would mean fetching a live secret merely to check a box, which
// section C of this feature's design explicitly forbids. Those references
// are reported NOT CHECKED rather than silently skipped or falsely passed.
func checkSecretExistence(refs []dsoReference) []setup.DoctorCheck {
	if len(refs) == 0 {
		return nil
	}

	type key struct{ project, path string }
	seen := map[key]bool{}
	var unique []key
	for _, r := range refs {
		k := key{r.Project, r.Path}
		if !seen[k] {
			seen[k] = true
			unique = append(unique, k)
		}
	}

	v, err := vault.LoadDefault()
	if err != nil {
		return []setup.DoctorCheck{{
			ID: "DSO-VALIDATE-006", Category: validateCatSecrets, Status: setup.DoctorInfo,
			Name: "Secret existence", Description: "Whether each referenced secret exists in its backing store",
			Detail: fmt.Sprintf("NOT CHECKED / local vault unavailable (%v) -- run 'docker dso init' or verify DSO_MASTER_KEY", err),
		}}
	}

	var missing []string
	for _, k := range unique {
		if _, getErr := v.Get(k.project, k.path); getErr != nil {
			missing = append(missing, k.project+"/"+k.path)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		return []setup.DoctorCheck{{
			ID: "DSO-VALIDATE-006", Category: validateCatSecrets, Status: setup.DoctorFail, Severity: setup.DoctorHigh,
			Name: "Secret existence", Description: "Whether each referenced secret exists in its backing store",
			Detail:    fmt.Sprintf("%d of %d referenced secret(s) not found: %s", len(missing), len(unique), strings.Join(missing, ", ")),
			RootCause: "A Compose file references a secret path that has not been imported/set in the vault",
			Recovery:  []string{"Run 'docker dso migrate' or 'docker dso secret set <project>/<path>' for the missing secret(s)"},
		}}
	}
	return []setup.DoctorCheck{{
		ID: "DSO-VALIDATE-006", Category: validateCatSecrets, Status: setup.DoctorPass,
		Name: "Secret existence", Description: "Whether each referenced secret exists in its backing store",
		Detail: fmt.Sprintf("%d referenced secret(s), all present", len(unique)),
	}}
}
