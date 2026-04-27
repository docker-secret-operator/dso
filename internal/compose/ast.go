package compose

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// GetMapValue safely retrieves a child node by key from a MappingNode.
func GetMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// SetMapValue safely sets or updates a key-value pair in a MappingNode.
func SetMapValue(node *yaml.Node, key string, valueNode *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = valueNode
			return
		}
	}
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: key,
		Tag:   "!!str",
	}
	node.Content = append(node.Content, keyNode, valueNode)
}

// ExtractUIDGID parses "uid:gid" or "uid" string into integers.
func ExtractUIDGID(userStr string) (int, int) {
	if userStr == "" {
		return 0, 0
	}
	parts := strings.Split(userStr, ":")
	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0 // fallback safely
	}
	gid := uid
	if len(parts) > 1 {
		parsedGid, err := strconv.Atoi(parts[1])
		if err == nil {
			gid = parsedGid
		}
	}
	return uid, gid
}

// AddTmpfsMount injects the /run/secrets/dso tmpfs mount into a service safely.
func AddTmpfsMount(serviceNode *yaml.Node) {
	mountPath := "/run/secrets/dso"
	tmpfsNode := GetMapValue(serviceNode, "tmpfs")

	if tmpfsNode == nil {
		tmpfsNode = &yaml.Node{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}
		SetMapValue(serviceNode, "tmpfs", tmpfsNode)
	}

	if tmpfsNode.Kind == yaml.SequenceNode {
		// Check for duplicates
		for _, item := range tmpfsNode.Content {
			if item.Value == mountPath {
				return
			}
		}
		// Append if missing
		tmpfsNode.Content = append(tmpfsNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: mountPath,
			Tag:   "!!str",
		})
	}
}
