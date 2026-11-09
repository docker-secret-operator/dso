package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler() *Handler {
	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>placeholder index</html>")},
		"foo.txt":    &fstest.MapFile{Data: []byte("hello")},
		"dir/bar.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	return NewHandler(fsys)
}

func TestHandler_ServesRealFile(t *testing.T) {
	h := testHandler()
	req := httptest.NewRequest(http.MethodGet, "/foo.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("unexpected body: %q", rec.Body.String())
	}
}

func TestHandler_ServesNestedFile(t *testing.T) {
	h := testHandler()
	req := httptest.NewRequest(http.MethodGet, "/dir/bar.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_RootServesIndex(t *testing.T) {
	h := testHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "placeholder index") {
		t.Errorf("expected index.html content, got: %q", rec.Body.String())
	}
}

func TestHandler_SPAFallback_UnknownRouteServesIndex(t *testing.T) {
	h := testHandler()
	for _, p := range []string{"/dashboard", "/security/events", "/some/deep/client/route"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("path %s: expected 200 (SPA fallback), got %d", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "placeholder index") {
			t.Errorf("path %s: expected index.html fallback content, got: %q", p, rec.Body.String())
		}
	}
}

// TestHandler_PathTraversal verifies that requests attempting to escape the
// embedded filesystem never succeed and never leak content outside fsys.
// fs.FS (via fstest.MapFS and, in production, fs.Sub over embed.FS) rejects
// ".." path elements at the Open() layer per io/fs's ValidPath rules, so
// these requests must all land on the SPA fallback (200, index content) or a
// clean error -- never on real filesystem content outside the embed root.
func TestHandler_PathTraversal(t *testing.T) {
	h := testHandler()
	attempts := []string{
		"/../../../etc/passwd",
		"/..%2f..%2f..%2fetc%2fpasswd",
		"/%2e%2e/%2e%2e/etc/passwd",
		"/foo.txt/../../../../etc/passwd",
		"/....//....//etc/passwd",
	}

	for _, p := range attempts {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		body := rec.Body.String()
		if strings.Contains(body, "root:") {
			t.Fatalf("path %q: response body appears to contain /etc/passwd content: %q", p, body)
		}
		// Every traversal attempt must resolve to either the SPA fallback
		// (200, our own placeholder content) or a clean 4xx -- never content
		// we didn't embed ourselves.
		if rec.Code == http.StatusOK && !strings.Contains(body, "placeholder index") {
			t.Fatalf("path %q: got 200 with unexpected body: %q", p, body)
		}
	}
}

func TestHandler_UnknownFileFallsBackToIndex(t *testing.T) {
	h := testHandler()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.png", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (SPA fallback), got %d", rec.Code)
	}
}
