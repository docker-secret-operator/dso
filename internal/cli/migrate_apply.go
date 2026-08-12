package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compose"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"gopkg.in/yaml.v3"
)

// ImportSummary reports what happened to each selected candidate. It is
// built incrementally as import proceeds so that a partial failure still
// yields an accurate, final accounting -- callers must never report
// "migration succeeded" without checking Failed is empty. It never holds a
// secret value, only key names and error text.
type ImportSummary struct {
	Imported       []string
	AlreadyExisted []string
	Skipped        []string
	Failed         map[string]string
}

func newImportSummary() *ImportSummary {
	return &ImportSummary{Failed: make(map[string]string)}
}

// applySecrets imports the plan's selected candidates into v, one key at a
// time (not vault.SetBatch, which is all-or-nothing and gives no per-key
// accounting). Each key's outcome is independent and deterministic:
//
//   - candidate not selected                    -> not touched, not reported
//   - exists in vault with an identical value    -> AlreadyExisted (no write)
//   - exists in vault with a different value,
//     overwrite=false (the default)              -> Skipped (no write)
//   - exists in vault with a different value,
//     overwrite=true                             -> Imported (write)
//   - does not exist in vault                    -> Imported (write)
//   - vault.Set returns an error                  -> Failed (no partial state
//     for that key; other keys are unaffected and continue processing)
//
// A failure on one key never aborts the remaining keys and never rolls
// back keys already written -- each vault.Set is independently persisted,
// so the only correct behavior is to keep going and report exactly what
// happened to each key.
func applySecrets(v *vault.Vault, plan *MigrationPlan, overwrite bool) *ImportSummary {
	summary := newImportSummary()

	for _, c := range plan.Candidates {
		if !c.Selected {
			continue
		}

		value := plan.envValues[c.Key]

		if c.ExistsInVault {
			if !c.VaultDiffers {
				summary.AlreadyExisted = append(summary.AlreadyExisted, c.Key)
				continue
			}
			if !overwrite {
				summary.Skipped = append(summary.Skipped, c.Key)
				continue
			}
		}

		if err := v.Set(plan.ProjectName, c.Key, value); err != nil {
			summary.Failed[c.Key] = err.Error()
			continue
		}
		summary.Imported = append(summary.Imported, c.Key)
	}

	return summary
}

// writeMigratedCompose reads composePath fresh, applies changes (each
// identifying a service + environment key to replace with a dso:// URI),
// and writes the result to outputPath. The original compose file is never
// opened for writing and is therefore never at risk, regardless of any
// error partway through -- either outputPath is written completely and
// correctly, or it is not written at all.
func writeMigratedCompose(composePath, outputPath string, changes []ComposeChange) error {
	content, err := os.ReadFile(composePath) // #nosec G304 -- composePath is caller-validated before this is called
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", composePath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return fmt.Errorf("failed to parse %s: %w", composePath, err)
	}

	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	servicesNode := compose.GetMapValue(doc, "services")
	if servicesNode == nil {
		return fmt.Errorf("no services: block found in %s", composePath)
	}

	byService := map[string][]ComposeChange{}
	for _, c := range changes {
		byService[c.Service] = append(byService[c.Service], c)
	}

	for i := 0; i+1 < len(servicesNode.Content); i += 2 {
		serviceName := servicesNode.Content[i].Value
		want, ok := byService[serviceName]
		if !ok {
			continue
		}
		serviceBody := servicesNode.Content[i+1]
		envNode := compose.GetMapValue(serviceBody, "environment")
		if envNode == nil {
			continue
		}
		applyEnvChanges(envNode, want)
	}

	out, err := os.Create(outputPath) // #nosec G304 -- outputPath is a fixed, caller-controlled filename (docker-compose.dso.yml), not user input
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", outputPath, err)
	}
	defer func() { _ = out.Close() }()

	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("failed to write %s: %w", outputPath, err)
	}
	return enc.Close()
}

// applyEnvChanges mutates envNode in place, replacing the value of each
// requested key (mapping form: `KEY: value`, or list form: `- KEY=value`)
// with its NewURI.
func applyEnvChanges(envNode *yaml.Node, changes []ComposeChange) {
	byKey := map[string]string{}
	for _, c := range changes {
		byKey[c.EnvKey] = c.NewURI
	}

	switch envNode.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(envNode.Content); i += 2 {
			key := envNode.Content[i].Value
			if newURI, ok := byKey[key]; ok {
				envNode.Content[i+1].Value = newURI
				envNode.Content[i+1].Tag = "!!str"
			}
		}
	case yaml.SequenceNode:
		for _, item := range envNode.Content {
			if item.Kind != yaml.ScalarNode {
				continue
			}
			parts := strings.SplitN(item.Value, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if newURI, ok := byKey[parts[0]]; ok {
				item.Value = parts[0] + "=" + newURI
			}
		}
	}
}
