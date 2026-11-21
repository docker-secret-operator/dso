package rotation

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"
)

func readTar(t *testing.T, b []byte) map[string]*tar.Header {
	t.Helper()
	out := map[string]*tar.Header{}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = h
	}
}

func TestBuildSecretTar_ModeOwnerAndPath(t *testing.T) {
	b, err := buildSecretTar("/run/secrets/dso", map[string]string{"db": "v1", "api": "v2"}, 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	h := readTar(t, b)
	for _, n := range []string{"/run/secrets/dso/db", "/run/secrets/dso/api"} {
		e := h[n]
		if e == nil {
			t.Fatalf("missing %s in %v", n, h)
		}
		if e.Mode != 0o400 || e.Uid != 1000 || e.Gid != 2000 || e.Typeflag != tar.TypeReg {
			t.Fatalf("%s: mode=%o uid=%d gid=%d type=%c", n, e.Mode, e.Uid, e.Gid, e.Typeflag)
		}
	}
}

func TestBuildSecretTar_RejectsUnsafeInput(t *testing.T) {
	cases := map[string]struct {
		target string
		data   map[string]string
		uid    int
	}{
		"empty name":         {"/run/secrets/dso", map[string]string{"": "v"}, 0},
		"dot name":           {"/run/secrets/dso", map[string]string{".": "v"}, 0},
		"dotdot name":        {"/run/secrets/dso", map[string]string{"..": "v"}, 0},
		"trailing slash dot": {"/run/secrets/dso", map[string]string{"a/..": "v"}, 0},
		"nul in name":        {"/run/secrets/dso", map[string]string{"a\x00b": "v"}, 0},
		"colliding names":    {"/run/secrets/dso", map[string]string{"x/db": "1", "y/db": "2"}, 0},
		"root target":        {"/", map[string]string{"a": "v"}, 0},
		"relative target":    {"run/secrets", map[string]string{"a": "v"}, 0},
		"unclean target":     {"/run/secrets/../etc", map[string]string{"a": "v"}, 0},
		"trailing slash":     {"/run/secrets/", map[string]string{"a": "v"}, 0},
		"negative uid":       {"/run/secrets/dso", map[string]string{"a": "v"}, -1},
	}
	for n, c := range cases {
		t.Run(n, func(t *testing.T) {
			if _, err := buildSecretTar(c.target, c.data, c.uid, 0); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestBuildSecretTar_TraversalIsConfinedToTarget(t *testing.T) {
	b, err := buildSecretTar("/run/secrets/dso", map[string]string{"../../etc/passwd": "v"}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for name := range readTar(t, b) {
		if !strings.HasPrefix(name, "/run/secrets/dso/") || strings.Contains(name, "..") {
			t.Fatalf("entry escaped the target: %s", name)
		}
	}
}
