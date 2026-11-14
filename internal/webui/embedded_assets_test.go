package webui

import (
	"bytes"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// These tests exercise the REAL embedded frontend (Assets(), i.e. what ships
// in the dso binary), not an injected fake. They guard against the
// generated-assets drift class of failure: an index/route HTML document that
// references a chunk which is not present in the embedded filesystem (for
// example because the chunk was never committed, is hidden by .gitignore, or
// is dropped by go:embed's rules for names starting with "_" or "."). In that
// case the Handler's SPA fallback would answer the missing script with HTML.

var (
	// Absolute references emitted into href/src attributes and inline preload data.
	absStaticRef = regexp.MustCompile(`/_next/static/[A-Za-z0-9_@~.\-/]+\.[A-Za-z0-9]+`)
	// App-router flight payload lists chunks relative to /_next/.
	relStaticRef = regexp.MustCompile(`"(static/(?:chunks|css|media)/[A-Za-z0-9_@~.\-/]+\.[A-Za-z0-9]+)"`)
)

func embeddedFS(t *testing.T) fs.FS {
	t.Helper()
	fsys, err := Assets()
	if err != nil {
		t.Fatalf("Assets(): %v", err)
	}
	return fsys
}

// staticRefs returns the sorted, de-duplicated "_next/static/..." FS names
// referenced by an HTML document.
func staticRefs(doc []byte) []string {
	set := map[string]struct{}{}
	for _, m := range absStaticRef.FindAll(doc, -1) {
		set[strings.TrimPrefix(string(m), "/")] = struct{}{}
	}
	for _, m := range relStaticRef.FindAllSubmatch(doc, -1) {
		set["_next/"+string(m[1])] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// missingRefs returns referenced names that are absent from fsys.
func missingRefs(fsys fs.FS, doc []byte) []string {
	var missing []string
	for _, name := range staticRefs(doc) {
		if _, err := fs.Stat(fsys, name); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func mediaType(rec *httptest.ResponseRecorder) string {
	mt, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	return mt
}

func TestEmbeddedAssets_IndexServedAsHTML(t *testing.T) {
	h := NewHandler(embeddedFS(t))
	for _, target := range []string{"/", "/index.html"} {
		rec := get(h, target)
		// http.FileServer redirects /index.html -> ./ ; accept only a final 200 for "/".
		if target == "/" {
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status %d, want 200", target, rec.Code)
			}
			if mt := mediaType(rec); mt != "text/html" {
				t.Fatalf("GET %s: content-type %q, want text/html", target, mt)
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte("/_next/static/")) {
				t.Fatalf("GET %s: body does not reference /_next/static/ (not the built dashboard)", target)
			}
		}
	}
}

func TestEmbeddedAssets_EveryReferencedChunkIsEmbeddedAndServed(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)

	docs, err := fs.Glob(fsys, "*.html")
	if err != nil || len(docs) == 0 {
		t.Fatalf("no embedded *.html documents (err=%v)", err)
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Fatalf("embedded index.html missing: %v", err)
	}

	seen := map[string]bool{}
	for _, doc := range docs {
		body, err := fs.ReadFile(fsys, doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		refs := staticRefs(body)
		if len(refs) == 0 && doc != "404.html" {
			t.Errorf("%s references no /_next/static/ files; unexpected for a built page", doc)
		}
		for _, name := range refs {
			if seen[name] {
				continue
			}
			seen[name] = true

			want, err := fs.ReadFile(fsys, name)
			if err != nil {
				t.Errorf("%s references %q which is NOT in the embedded filesystem", doc, name)
				continue
			}
			rec := get(h, "/"+name)
			if rec.Code != http.StatusOK {
				t.Errorf("GET /%s (from %s): status %d, want 200", name, doc, rec.Code)
				continue
			}
			ct := mediaType(rec)
			switch path.Ext(name) {
			case ".js":
				if ct != "text/javascript" && ct != "application/javascript" {
					t.Errorf("GET /%s: content-type %q, want JavaScript", name, ct)
				}
			case ".css":
				if ct != "text/css" {
					t.Errorf("GET /%s: content-type %q, want text/css", name, ct)
				}
			}
			if ct == "text/html" {
				t.Errorf("GET /%s answered with HTML (SPA fallback masking a missing asset)", name)
			}
			if !bytes.Equal(rec.Body.Bytes(), want) {
				t.Errorf("GET /%s: served body differs from the embedded file", name)
			}
		}
	}
	if len(seen) < 10 {
		t.Errorf("only %d distinct static references found across %d documents; extraction looks broken", len(seen), len(docs))
	}
}

// Every route linked from index.html (e.g. /secrets) must be served by its own
// prerendered document, not by the SPA index fallback.
func TestEmbeddedAssets_LinkedRoutesHaveOwnDocument(t *testing.T) {
	fsys := embeddedFS(t)
	h := NewHandler(fsys)
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	routeRef := regexp.MustCompile(`href="/([a-z]+)"`)
	routes := map[string]bool{}
	for _, m := range routeRef.FindAllSubmatch(index, -1) {
		routes[string(m[1])] = true
	}
	if len(routes) == 0 {
		t.Fatal("no route links found in index.html")
	}
	for r := range routes {
		want, err := fs.ReadFile(fsys, r+".html")
		if err != nil {
			t.Errorf("route /%s linked from index.html has no embedded %s.html", r, r)
			continue
		}
		rec := get(h, "/"+r)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Errorf("GET /%s: status %d or body is not %s.html", r, rec.Code, r)
		}
	}
}

func TestEmbeddedAssets_SPAFallbackServesIndex(t *testing.T) {
	fsys := embeddedFS(t)
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	rec := get(NewHandler(fsys), "/no/such/client/route")
	if rec.Code != http.StatusOK || mediaType(rec) != "text/html" || !bytes.Equal(rec.Body.Bytes(), index) {
		t.Fatalf("unknown route: status %d type %q; want 200 text/html equal to index.html", rec.Code, mediaType(rec))
	}
}

// Detection logic must flag a chunk referenced by index.html but absent from
// the filesystem (the exact failure mode of drifted/partially-committed assets).
func TestMissingRefs_DetectsReferencedChunkAbsentFromFS(t *testing.T) {
	doc := []byte(`<script src="/_next/static/chunks/app/secrets/page-deadbeef.js" async></script>` +
		`<link rel="stylesheet" href="/_next/static/css/present.css"/>` +
		`<script>self.__next_f.push([1,["1","static/chunks/app/gone-1234.js"]])</script>`)
	fsys := fstest.MapFS{
		"index.html":                   {Data: doc},
		"_next/static/css/present.css": {Data: []byte("a{}")},
	}
	got := missingRefs(fsys, doc)
	want := []string{"_next/static/chunks/app/gone-1234.js", "_next/static/chunks/app/secrets/page-deadbeef.js"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("missingRefs = %v, want %v", got, want)
	}
	if len(missingRefs(fstest.MapFS{"_next/static/css/present.css": {Data: nil}}, []byte(`href="/_next/static/css/present.css"`))) != 0 {
		t.Fatal("present chunk reported missing")
	}
}
