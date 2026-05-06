package resolver

import (
	"context"
	"testing"

	"github.com/docker-secret-operator/dso/test/testutil"
	"gopkg.in/yaml.v3"
)

func TestParseURIPath(t *testing.T) {
	tests := []struct {
		uri      string
		fallback string
		proj     string
		path     string
		err      bool
	}{
		{"myproj/mysec", "fall", "myproj", "mysec", false},
		{"mysec", "fall", "fall", "mysec", false},
		{"", "fall", "", "", true},
		{"/", "fall", "", "", true},
		{"proj/", "fall", "proj", "", true},
	}
	for _, tt := range tests {
		p, pth, err := parseURIPath(tt.uri, tt.fallback)
		if (err != nil) != tt.err {
			t.Errorf("parseURIPath(%q, %q) error = %v, wantErr %v", tt.uri, tt.fallback, err, tt.err)
		}
		if !tt.err && (p != tt.proj || pth != tt.path) {
			t.Errorf("parseURIPath(%q, %q) = %q, %q, want %q, %q", tt.uri, tt.fallback, p, pth, tt.proj, tt.path)
		}
	}
}

func TestResolveCompose(t *testing.T) {
	tv := testutil.NewTempVault(t)
	tv.Vault.Set("myproj", "db_pass", "secret1")
	tv.Vault.Set("myproj", "db_cert", "secret2")
	yamlContent := `
services:
  web:
    image: nginx
    environment:
      DB_PASS: dso://myproj/db_pass
      FILE_SEC: dsofile://myproj/db_cert
      NORMAL_ENV: some_value
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		t.Fatal(err)
	}

	_, seed, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err != nil {
		t.Fatal(err)
	}
	if seed == nil {
		t.Fatal("seed is nil")
	}
	if len(seed.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(seed.Services))
	}
	sec := seed.Services["web"]
	if len(sec.EnvSecrets) != 1 || len(sec.FileSecrets) != 1 {
		t.Fatalf("unexpected secrets map lengths")
	}
}

func TestResolveComposeWithSequenceEnv(t *testing.T) {
	tv := testutil.NewTempVault(t)
	tv.Vault.Set("myproj", "db_pass", "secret1")
	yamlContent := `
services:
  web:
    image: nginx
    environment:
      - DB_PASS=dso://myproj/db_pass
      - NORMAL_ENV=val
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		t.Fatal(err)
	}

	_, seed, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err != nil {
		t.Fatal(err)
	}
	if len(seed.Services["web"].EnvSecrets) != 1 {
		t.Fatalf("expected 1 env secret")
	}
}

func TestResolveComposeInvalidURI(t *testing.T) {
	tv := testutil.NewTempVault(t)
	yamlContent := `
services:
  web:
    image: nginx
    environment:
      DB_PASS: dso://
`
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &root); err != nil {
		t.Fatal(err)
	}

	_, _, err := ResolveCompose(context.Background(), nil, &root, tv.Vault, "myproj")
	if err == nil {
		t.Fatal("expected error for invalid uri")
	}
}

func TestHashSecret(t *testing.T) {
	h1 := hashSecret("proj", "path", "val")
	h2 := hashSecret("proj", "path", "val")
	if h1 != h2 {
		t.Fatal("hash should be deterministic")
	}
}
