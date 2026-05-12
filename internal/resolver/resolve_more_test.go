package resolver

import (
	"context"
	"github.com/docker-secret-operator/dso/test/testutil"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestResolveComposeWithCommandString(t *testing.T) {
	tv := testutil.NewTempVault(t)
	tv.Vault.Set("myproj", "db_cert", "secret2")
	yamlContent := `
services:
  web:
    image: nginx
    command: "start-web.sh"
    environment:
      FILE_SEC: dsofile://myproj/db_cert
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveComposeWithCommandSequence(t *testing.T) {
	tv := testutil.NewTempVault(t)
	tv.Vault.Set("myproj", "db_cert", "secret2")
	yamlContent := `
services:
  web:
    image: nginx
    command: ["start-web.sh", "--arg"]
    environment:
      FILE_SEC: dsofile://myproj/db_cert
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveComposeMissingEnvHandling(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    image: nginx
    environment:
      MISSING: dso://myproj/nonexistent
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveCompose_RootErrors(t *testing.T) {
	tv := testutil.NewTempVault(t)
	_, _, err := ResolveCompose(context.Background(), nil, nil, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected err for nil root")
	}

	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "invalid"}
	_, _, err = ResolveCompose(context.Background(), nil, scalar, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected err for scalar root")
	}
}

func TestResolveCompose_MissingVaultSecret(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    environment:
      MISSING: dso://myproj/nonexistent
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestResolveCompose_MissingVaultSecretFile(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    environment:
      MISSING: dsofile://myproj/nonexistent
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestWrapCommandWithWait_Fallback(t *testing.T) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	wrapCommandWithWait(node, "test-srv", map[string]string{"/file": "hash"}, []string{"orig-entry"}, []string{"orig-cmd"})
	// command should be added
	if len(node.Content) < 2 || node.Content[0].Value != "command" {
		t.Fatal("expected command to be set")
	}
}

func TestResolveCompose_InvalidEnvList(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    environment:
      - INVALID_ENV_NO_EQUALS
      - VALID=dso://myproj/nonexistent
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected error for missing secret on VALID")
	}
}

func TestResolveCompose_UIDGIDS(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    user: "1000:2000"
    environment:
      - VALID=test
`
	var root yaml.Node
	_ = yaml.Unmarshal([]byte(yamlContent), &root)

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err != nil {
		t.Fatal(err)
	}
}
