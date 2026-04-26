package resolver

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/compose"
	"github.com/docker-secret-operator/dso/pkg/vault"
	"gopkg.in/yaml.v3"
)

type AgentSeed struct {
	ProjectName string
	SecretPool  map[string]string
	Services    map[string]ServiceSecrets
}

type ServiceSecrets struct {
	UID         int
	GID         int
	EnvSecrets  map[string]string
	FileSecrets map[string]string // path → hash
}

// hashSecret creates a deterministic hash for deduplication in the SecretPool.
func hashSecret(project, path, value string) string {
	data := fmt.Sprintf("%s:%s:%s", project, path, value)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
}

// parseURIPath resolves the target project and path from a URI path.
// If the path contains no slash, it defaults to the given fallback project.
func parseURIPath(uriPath, fallbackProject string) (string, string, error) {
	parts := strings.SplitN(uriPath, "/", 2)
	var project, path string
	
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
		return "", "", fmt.Errorf("invalid URI format: project or path is empty")
	}

	return project, path, nil
}

// ResolveCompose traverses a parsed Docker Compose YAML AST, detects DSO URIs,
// fetches secrets from the Vault, mutates the AST in-place, and builds the AgentSeed.
func ResolveCompose(root *yaml.Node, v *vault.Vault, composeProject string) (*yaml.Node, *AgentSeed, error) {
	if root == nil {
		return nil, nil, fmt.Errorf("root node is nil")
	}

	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("expected mapping node at yaml root")
	}

	servicesNode := compose.GetMapValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		// Nothing to resolve
		return root, &AgentSeed{ProjectName: composeProject}, nil
	}

	seed := &AgentSeed{
		ProjectName: composeProject,
		SecretPool:  make(map[string]string),
		Services:    make(map[string]ServiceSecrets),
	}

	for i := 0; i < len(servicesNode.Content); i += 2 {
		serviceName := servicesNode.Content[i].Value
		serviceBody := servicesNode.Content[i+1]

		if serviceBody.Kind != yaml.MappingNode {
			continue
		}

		userNode := compose.GetMapValue(serviceBody, "user")
		uid, gid := 0, 0
		if userNode != nil && userNode.Kind == yaml.ScalarNode {
			uid, gid = compose.ExtractUIDGID(userNode.Value)
		}

		serviceSecrets := ServiceSecrets{
			UID:         uid,
			GID:         gid,
			EnvSecrets:  make(map[string]string),
			FileSecrets: make(map[string]string),
		}

		envNode := compose.GetMapValue(serviceBody, "environment")
		if envNode != nil {
			if err := resolveEnvironment(envNode, v, composeProject, serviceName, &serviceSecrets, seed); err != nil {
				return nil, nil, fmt.Errorf("service '%s': %w", serviceName, err)
			}
		}

		// Inject tmpfs if the service requires file secrets
		if len(serviceSecrets.FileSecrets) > 0 {
			compose.AddTmpfsMount(serviceBody)
		}

		if len(serviceSecrets.EnvSecrets) > 0 || len(serviceSecrets.FileSecrets) > 0 {
			seed.Services[serviceName] = serviceSecrets
		}
	}

	return root, seed, nil
}

func resolveEnvironment(envNode *yaml.Node, v *vault.Vault, composeProject, serviceName string, serviceSecrets *ServiceSecrets, seed *AgentSeed) error {
	switch envNode.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(envNode.Content); i += 2 {
			keyNode := envNode.Content[i]
			valNode := envNode.Content[i+1]

			if valNode.Kind != yaml.ScalarNode {
				continue
			}

			newVal, handled, err := processSecretURI(valNode.Value, v, composeProject, serviceName, keyNode.Value, serviceSecrets, seed)
			if err != nil {
				return err
			}
			if handled {
				valNode.Value = newVal
			}
		}

	case yaml.SequenceNode:
		for _, itemNode := range envNode.Content {
			if itemNode.Kind != yaml.ScalarNode {
				continue
			}

			parts := strings.SplitN(itemNode.Value, "=", 2)
			if len(parts) != 2 {
				continue // Skip invalid env list entries
			}

			key := parts[0]
			val := parts[1]

			newVal, handled, err := processSecretURI(val, v, composeProject, serviceName, key, serviceSecrets, seed)
			if err != nil {
				return err
			}
			if handled {
				itemNode.Value = fmt.Sprintf("%s=%s", key, newVal)
			}
		}
	}

	return nil
}

func processSecretURI(uri string, v *vault.Vault, composeProject, serviceName, key string, serviceSecrets *ServiceSecrets, seed *AgentSeed) (string, bool, error) {
	if strings.HasPrefix(uri, "dso://") {
		uriPath := strings.TrimPrefix(uri, "dso://")
		
		targetProject, secretPath, err := parseURIPath(uriPath, composeProject)
		if err != nil {
			return "", false, fmt.Errorf("env key '%s': %w", key, err)
		}

		sec, err := v.Get(targetProject, secretPath)
		if err != nil {
			return "", false, fmt.Errorf("env key '%s': failed to read vault: %w", key, err)
		}

		// Secure logging: remove sensitive URI path from stdout
		fmt.Printf("⚠️  WARNING: Service '%s' is injecting a secret into environment variable '%s' via dso:// (Environment injection). This is visible in docker inspect.\n", serviceName, key)

		poolHash := hashSecret(targetProject, secretPath, sec.Value)
		seed.SecretPool[poolHash] = sec.Value
		serviceSecrets.EnvSecrets[key] = poolHash

		return sec.Value, true, nil

	} else if strings.HasPrefix(uri, "dsofile://") {
		uriPath := strings.TrimPrefix(uri, "dsofile://")
		
		targetProject, secretPath, err := parseURIPath(uriPath, composeProject)
		if err != nil {
			return "", false, fmt.Errorf("env key '%s': %w", key, err)
		}

		sec, err := v.Get(targetProject, secretPath)
		if err != nil {
			return "", false, fmt.Errorf("env key '%s': failed to read vault: %w", key, err)
		}

		// Generate deterministic file name: <service>_<hash(project:path)[:8]>
		hashInput := fmt.Sprintf("%s:%s", targetProject, secretPath)
		pathHash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:8]
		fileName := fmt.Sprintf("%s_%s", serviceName, pathHash)
		filePath := fmt.Sprintf("/run/secrets/dso/%s", fileName)

		poolHash := hashSecret(targetProject, secretPath, sec.Value)
		seed.SecretPool[poolHash] = sec.Value
		serviceSecrets.FileSecrets[filePath] = poolHash

		return filePath, true, nil
	}

	return "", false, nil
}
