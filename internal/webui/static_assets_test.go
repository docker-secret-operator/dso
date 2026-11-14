package webui

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests for static-asset serving against the REAL embedded
// filesystem: a missing asset must be a 404, never the SPA index.html.

// firstEmbedded returns the first embedded file under dir with the given
// extension, discovered programmatically (chunk hashes change per build).
func firstEmbedded(t *testing.T, fsys fs.FS, dir, ext string) string {
	t.Helper()
	var found string
	_ = fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && found == "" && !d.IsDir() && strings.HasSuffix(p, ext) {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Skipf("no embedded %s file under %s", ext, dir)
	}
	return found
}

func assertNotIndex(t *testing.T, target string, rec *httptest.ResponseRecorder, index []byte) {
	t.Helper()
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET %s: status %d, want 404", target, rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), index) || mediaType(rec) == "text/html" {
		t.Errorf("GET %s: response is index.html/HTML", target)
	}
}

func TestEmbeddedStatic_ExistingAssetsExactBytesAndTypes(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)

	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	rec := get(h, "/")
	if rec.Code != 200 || mediaType(rec) != "text/html" || !bytes.Equal(rec.Body.Bytes(), index) {
		t.Fatalf("GET /: status %d type %q, want 200 text/html with exact index bytes", rec.Code, mediaType(rec))
	}

	for _, c := range []struct{ dir, ext, major string }{
		{"_next/static/chunks", ".js", "javascript"},
		{"_next/static/css", ".css", "text/css"},
	} {
		name := firstEmbedded(t, fsys, c.dir, c.ext)
		want, err := fs.ReadFile(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		rec := get(h, "/"+name)
		if rec.Code != 200 {
			t.Fatalf("GET /%s: status %d, want 200", name, rec.Code)
		}
		if !bytes.Equal(rec.Body.Bytes(), want) {
			t.Errorf("GET /%s: body differs from embedded bytes", name)
		}
		mt := mediaType(rec)
		if !strings.Contains(mt, c.major) || mt == "text/html" {
			t.Errorf("GET /%s: content-type %q, want %s", name, mt, c.major)
		}
	}
}

func TestEmbeddedStatic_MissingAssetsAre404(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)
	index, _ := fs.ReadFile(fsys, "index.html")

	for _, target := range []string{
		"/_next/static/chunks/app/secrets/page-DOES-NOT-EXIST.js",
		"/_next/static/chunks/DOES-NOT-EXIST.js",
		"/_next/static/css/DOES-NOT-EXIST.css",
		"/_next/static/chunks/DOES-NOT-EXIST.js.map",
		"/_next/static/media/DOES-NOT-EXIST.woff2",
		"/_next/static/anything-extensionless",
		"/_next/DOES-NOT-EXIST",
		"/_next/DOES-NOT-EXIST.json",
		"/DOES-NOT-EXIST.js",
		"/DOES-NOT-EXIST.css",
		"/DOES-NOT-EXIST.map",
		"/DOES-NOT-EXIST.woff2",
		"/DOES-NOT-EXIST.png",
		"/DOES-NOT-EXIST.ico",
		"/DOES-NOT-EXIST.json",
		"/DOES-NOT-EXIST.txt",
		"/some/dir/DOES-NOT-EXIST.JS",
	} {
		rec := get(h, target)
		assertNotIndex(t, target, rec, index)
		if b := rec.Body.String(); strings.Contains(b, "assets") || strings.Contains(b, "embed") {
			t.Errorf("GET %s: 404 body leaks internals: %q", target, b)
		}
	}
}

func TestEmbeddedStatic_NavigationFallbackPreserved(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatal(err)
	}

	// Known route with its own prerendered document.
	if want, err := fs.ReadFile(fsys, "secrets.html"); err == nil {
		rec := get(h, "/secrets")
		if rec.Code != 200 || mediaType(rec) != "text/html" || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Errorf("GET /secrets: status %d type %q, want 200 text/html secrets.html", rec.Code, mediaType(rec))
		}
	}

	// Unknown extensionless routes, including dotted-directory and traversal
	// shapes, still get the SPA shell.
	for _, target := range []string{"/unknown-spa-route", "/no/such/client/route", "/foo/", "/foo/../bar"} {
		rec := get(h, target)
		if rec.Code != 200 || mediaType(rec) != "text/html" || !bytes.Equal(rec.Body.Bytes(), index) {
			t.Errorf("GET %s: status %d type %q, want 200 text/html index", target, rec.Code, mediaType(rec))
		}
	}
}

func TestEmbeddedStatic_TraversalAndMethods(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)
	index, _ := fs.ReadFile(fsys, "index.html")

	for _, target := range []string{
		"/../../../etc/passwd",
		"/..%2f..%2f..%2fetc%2fpasswd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/_next/static/../../../../etc/passwd",
	} {
		rec := get(h, target)
		if strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("GET %s leaked file content", target)
		}
		if rec.Code == 200 && !bytes.Equal(rec.Body.Bytes(), index) {
			t.Errorf("GET %s: 200 with non-index body", target)
		}
	}

	// HEAD: same status as GET (recorder does not strip 404 bodies; net/http does).
	js := firstEmbedded(t, fsys, "_next/static/chunks", ".js")
	for target, want := range map[string]int{
		"/" + js:                          200,
		"/unknown-spa-route":              200,
		"/_next/static/chunks/NOPE.js":    404,
		"/_next/static/chunks/NOPE.woff2": 404,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, target, nil))
		if rec.Code != want || (want == 200 && rec.Body.Len() != 0) {
			t.Errorf("HEAD %s: status %d body %d bytes, want %d empty", target, rec.Code, rec.Body.Len(), want)
		}
	}
}
