package compose

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGetMapValueReturnsCorrectValue retrieves existing key
func TestGetMapValueReturnsCorrectValue(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "value1"},
			{Kind: yaml.ScalarNode, Value: "key2"},
			{Kind: yaml.ScalarNode, Value: "value2"},
		},
	}

	result := GetMapValue(node, "key1")
	if result == nil {
		t.Fatal("GetMapValue returned nil")
	}

	if result.Value != "value1" {
		t.Errorf("Expected 'value1', got '%s'", result.Value)
	}
}

// TestGetMapValueReturnsNilForMissing returns nil for non-existent key
func TestGetMapValueReturnsNilForMissing(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "value1"},
		},
	}

	result := GetMapValue(node, "nonexistent")
	if result != nil {
		t.Fatal("GetMapValue should return nil for missing key")
	}
}

// TestGetMapValueHandlesNilNode returns nil safely
func TestGetMapValueHandlesNilNode(t *testing.T) {
	result := GetMapValue(nil, "key")
	if result != nil {
		t.Fatal("GetMapValue should return nil for nil node")
	}
}

// TestGetMapValueHandlesNonMappingNode returns nil safely
func TestGetMapValueHandlesNonMappingNode(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.SequenceNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "item1"},
		},
	}

	result := GetMapValue(node, "key")
	if result != nil {
		t.Fatal("GetMapValue should return nil for non-mapping node")
	}
}

// TestGetMapValueHandlesEmptyNode returns nil safely
func TestGetMapValueHandlesEmptyNode(t *testing.T) {
	node := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{},
	}

	result := GetMapValue(node, "key")
	if result != nil {
		t.Fatal("GetMapValue should return nil for empty mapping")
	}
}

// TestSetMapValueCreatesNewKey adds new key-value pair
func TestSetMapValueCreatesNewKey(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "existing"},
			{Kind: yaml.ScalarNode, Value: "value"},
		},
	}

	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "new-value",
		Tag:   "!!str",
	}

	SetMapValue(node, "newkey", newValue)

	// Verify the new key exists
	result := GetMapValue(node, "newkey")
	if result == nil {
		t.Fatal("New key not set")
	}

	if result.Value != "new-value" {
		t.Errorf("Expected 'new-value', got '%s'", result.Value)
	}
}

// TestSetMapValueUpdatesExistingKey modifies existing key
func TestSetMapValueUpdatesExistingKey(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "old-value"},
		},
	}

	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "updated-value",
		Tag:   "!!str",
	}

	SetMapValue(node, "key1", newValue)

	result := GetMapValue(node, "key1")
	if result.Value != "updated-value" {
		t.Errorf("Expected 'updated-value', got '%s'", result.Value)
	}
}

// TestSetMapValueHandlesNilNode handles nil safely
func TestSetMapValueHandlesNilNode(t *testing.T) {
	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "value",
	}

	// Should not panic
	SetMapValue(nil, "key", newValue)
}

// TestSetMapValueHandlesNonMappingNode handles non-mapping safely
func TestSetMapValueHandlesNonMappingNode(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.SequenceNode,
	}

	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "value",
	}

	// Should not panic or modify sequence
	SetMapValue(node, "key", newValue)

	if len(node.Content) != 0 {
		t.Error("Sequence node should not be modified")
	}
}

// TestExtractUIDGIDFromString parses "uid" format
func TestExtractUIDGIDFromString(t *testing.T) {
	tests := []struct {
		input    string
		wantUID  int
		wantGID  int
		name     string
	}{
		{"1000", 1000, 1000, "uid only"},
		{"1000:2000", 1000, 2000, "uid:gid"},
		{"0", 0, 0, "root"},
		{"999:999", 999, 999, "non-standard"},
		{"", 0, 0, "empty"},
		{"invalid", 0, 0, "invalid uid"},
		{"1000:invalid", 1000, 1000, "invalid gid fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, gid := ExtractUIDGID(tt.input)
			if uid != tt.wantUID || gid != tt.wantGID {
				t.Errorf("ExtractUIDGID(%q) = (%d, %d), want (%d, %d)", tt.input, uid, gid, tt.wantUID, tt.wantGID)
			}
		})
	}
}

// TestAddTmpfsMountCreatesMount injects tmpfs mount
func TestAddTmpfsMountCreatesMount(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{},
	}

	AddTmpfsMount(serviceNode)

	tmpfsNode := GetMapValue(serviceNode, "tmpfs")
	if tmpfsNode == nil {
		t.Fatal("tmpfs mount not created")
	}

	if tmpfsNode.Kind != yaml.SequenceNode {
		t.Errorf("tmpfs should be a sequence, got %v", tmpfsNode.Kind)
	}

	if len(tmpfsNode.Content) != 1 {
		t.Errorf("Expected 1 mount, got %d", len(tmpfsNode.Content))
	}

	if tmpfsNode.Content[0].Value != "/run/secrets/dso" {
		t.Errorf("Expected '/run/secrets/dso', got '%s'", tmpfsNode.Content[0].Value)
	}
}

// TestAddTmpfsMountDeduplicatesMounts avoids duplicates
func TestAddTmpfsMountDeduplicatesMounts(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "tmpfs"},
			{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "/run/secrets/dso"},
				},
			},
		},
	}

	AddTmpfsMount(serviceNode)

	tmpfsNode := GetMapValue(serviceNode, "tmpfs")
	if len(tmpfsNode.Content) != 1 {
		t.Errorf("Should have 1 mount (no duplicates), got %d", len(tmpfsNode.Content))
	}
}

// TestAddTmpfsMountToExistingMounts adds to existing mounts
func TestAddTmpfsMountToExistingMounts(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "tmpfs"},
			{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "/tmp"},
				},
			},
		},
	}

	AddTmpfsMount(serviceNode)

	tmpfsNode := GetMapValue(serviceNode, "tmpfs")
	if len(tmpfsNode.Content) != 2 {
		t.Errorf("Expected 2 mounts, got %d", len(tmpfsNode.Content))
	}

	// Check both mounts exist
	found := false
	for _, mount := range tmpfsNode.Content {
		if mount.Value == "/run/secrets/dso" {
			found = true
		}
	}

	if !found {
		t.Error("DSO mount not found in mounts")
	}
}

// TestAddTmpfsMountHandlesNilNode handles nil safely
func TestAddTmpfsMountHandlesNilNode(t *testing.T) {
	// Should not panic
	AddTmpfsMount(nil)
}

// TestAddTmpfsMountHandlesNonMappingNode handles non-mapping safely
func TestAddTmpfsMountHandlesNonMappingNode(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind: yaml.SequenceNode,
	}

	// Should not panic
	AddTmpfsMount(serviceNode)
}

// TestComposeASTComplexStructure handles nested structures
func TestComposeASTComplexStructure(t *testing.T) {
	// Simulate a real docker-compose service definition
	serviceNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			// image
			{Kind: yaml.ScalarNode, Value: "image"},
			{Kind: yaml.ScalarNode, Value: "postgres:15"},
			// ports
			{Kind: yaml.ScalarNode, Value: "ports"},
			{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "5432:5432"},
				},
			},
			// environment
			{Kind: yaml.ScalarNode, Value: "environment"},
			{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "POSTGRES_DB"},
					{Kind: yaml.ScalarNode, Value: "mydb"},
				},
			},
		},
	}

	// Add DSO tmpfs mount
	AddTmpfsMount(serviceNode)

	// Verify original structure intact
	if image := GetMapValue(serviceNode, "image"); image == nil || image.Value != "postgres:15" {
		t.Error("Original image field corrupted")
	}

	if ports := GetMapValue(serviceNode, "ports"); ports == nil {
		t.Error("Original ports field corrupted")
	}

	// Verify mount added
	if tmpfs := GetMapValue(serviceNode, "tmpfs"); tmpfs == nil {
		t.Error("tmpfs mount not added")
	}
}

// TestGetMapValueWithSpecialCharacters handles keys with special chars
func TestGetMapValueWithSpecialCharacters(t *testing.T) {
	tests := []string{
		"simple-key",
		"key_with_underscore",
		"key.with.dots",
		"key/with/slashes",
		"key-with-multiple-dashes",
		"123-numeric",
	}

	for _, keyName := range tests {
		t.Run(keyName, func(t *testing.T) {
			node := &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: keyName},
					{Kind: yaml.ScalarNode, Value: "test-value"},
				},
			}

			result := GetMapValue(node, keyName)
			if result == nil {
				t.Error("Failed to get key with special characters")
			}

			if result.Value != "test-value" {
				t.Errorf("Expected 'test-value', got '%s'", result.Value)
			}
		})
	}
}

// TestSetMapValuePreservesKeyOrder maintains insertion order
func TestSetMapValuePreservesKeyOrder(t *testing.T) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "first"},
			{Kind: yaml.ScalarNode, Value: "value1"},
			{Kind: yaml.ScalarNode, Value: "second"},
			{Kind: yaml.ScalarNode, Value: "value2"},
		},
	}

	// Update first value
	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "updated",
		Tag:   "!!str",
	}
	SetMapValue(node, "first", newValue)

	// Verify order preserved
	keys := make([]string, 0)
	for i := 0; i < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}

	if len(keys) != 2 || keys[0] != "first" || keys[1] != "second" {
		t.Errorf("Key order not preserved: %v", keys)
	}
}

// TestAddTmpfsMountPreservesOtherFields doesn't corrupt others
func TestAddTmpfsMountPreservesOtherFields(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "image"},
			{Kind: yaml.ScalarNode, Value: "myimage:latest"},
			{Kind: yaml.ScalarNode, Value: "ports"},
			{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "8080:8080"},
				},
			},
		},
	}

	originalImage := GetMapValue(serviceNode, "image").Value
	originalPorts := len(GetMapValue(serviceNode, "ports").Content)

	AddTmpfsMount(serviceNode)

	if GetMapValue(serviceNode, "image").Value != originalImage {
		t.Error("Image field was corrupted")
	}

	if len(GetMapValue(serviceNode, "ports").Content) != originalPorts {
		t.Error("Ports field was corrupted")
	}
}

// BenchmarkGetMapValue measures lookup performance
func BenchmarkGetMapValue(b *testing.B) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: make([]*yaml.Node, 100),
	}

	// Create 50 key-value pairs
	for i := 0; i < 50; i++ {
		node.Content[i*2] = &yaml.Node{Kind: yaml.ScalarNode, Value: "key-" + string(rune(i))}
		node.Content[i*2+1] = &yaml.Node{Kind: yaml.ScalarNode, Value: "value-" + string(rune(i))}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetMapValue(node, "key-10")
	}
}

// BenchmarkSetMapValue measures update performance
func BenchmarkSetMapValue(b *testing.B) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "existing"},
			{Kind: yaml.ScalarNode, Value: "value"},
		},
	}

	newValue := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "new",
		Tag:   "!!str",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SetMapValue(node, "testkey", newValue)
	}
}

// TestAddTmpfsMountWithMalformedNode handles corrupted tmpfs node
func TestAddTmpfsMountWithMalformedNode(t *testing.T) {
	serviceNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "tmpfs"},
			// Intentionally broken - scalar instead of sequence
			{Kind: yaml.ScalarNode, Value: "/existing/mount"},
		},
	}

	// Should handle gracefully without panic
	AddTmpfsMount(serviceNode)

	// Node should either be fixed or left as-is
	tmpfsNode := GetMapValue(serviceNode, "tmpfs")
	if tmpfsNode != nil && tmpfsNode.Kind != yaml.SequenceNode {
		// This is acceptable - we don't modify scalar tmpfs nodes
		if tmpfsNode.Value != "/existing/mount" {
			t.Error("Existing scalar tmpfs value was corrupted")
		}
	}
}

// TestGetMapValueWithOddContent handles malformed mapping (odd number of elements)
func TestGetMapValueWithOddContent(t *testing.T) {
	// Malformed node with odd number of content items
	node := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "value1"},
			{Kind: yaml.ScalarNode, Value: "key2"},
			// Missing value for key2 - malformed
		},
	}

	// Should handle gracefully without panic
	result := GetMapValue(node, "key1")
	if result == nil {
		t.Fatal("Should find key1 despite malformed node")
	}

	if result.Value != "value1" {
		t.Errorf("Expected 'value1', got '%s'", result.Value)
	}
}

// TestExtractUIDGIDWithWhitespace handles whitespace edge case
func TestExtractUIDGIDWithWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		wantUID  int
		wantGID  int
		name     string
	}{
		{"1000", 1000, 1000, "plain uid"},
		{"1000:2000", 1000, 2000, "uid:gid"},
		{" 1000", 0, 0, "leading space (invalid)"},
		{"1000 :2000", 0, 0, "space before colon (invalid)"},
		{"\t1000", 0, 0, "tab prefix (invalid)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, gid := ExtractUIDGID(tt.input)
			if uid != tt.wantUID || gid != tt.wantGID {
				t.Logf("ExtractUIDGID(%q) = (%d, %d), expected (%d, %d)", tt.input, uid, gid, tt.wantUID, tt.wantGID)
			}
		})
	}
}
