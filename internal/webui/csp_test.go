package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func parseCSP(p string) map[string][]string {
	out := map[string][]string{}
	for _, d := range strings.Split(p, ";") {
		f := strings.Fields(strings.TrimSpace(d))
		if len(f) > 0 {
			out[f[0]] = f[1:]
		}
	}
	return out
}

func has(srcs []string, v string) bool {
	for _, s := range srcs {
		if s == v {
			return true
		}
	}
	return false
}

// sameOriginPath reports whether a reference is a same-origin absolute path
// (the only kind 'self' admits); external schemes are never allowed.
func sameOriginPath(ref string) bool {
	return strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "//")
}

var (
	tagRe       = regexp.MustCompile(`(?is)<(script|link|img|iframe|object|embed|form|base|meta)\b([^>]*)>`)
	attrRe      = regexp.MustCompile(`(?i)\b([a-z-]+)\s*=\s*"([^"]*)"`)
	onHandlerRe = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	cssURLRe    = regexp.MustCompile(`url\(([^)]*)\)`)
)

// TestEmbeddedPagesSatisfyTheirOwnCSP loads every embedded HTML document, takes
// the policy the handler serves for it, and verifies that every resource the
// page references is permitted by that policy and that nothing the policy
// forbids (inline event handlers, external origins, eval-requiring directives)
// is needed.
func TestEmbeddedPagesSatisfyTheirOwnCSP(t *testing.T) {
	fsys, err := Assets()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(fsys)
	var pages []string
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			pages = append(pages, p)
		}
		return nil
	})
	if len(pages) < 10 {
		t.Fatalf("expected the full set of embedded pages, found %d", len(pages))
	}

	for _, page := range pages {
		page := page
		t.Run(page, func(t *testing.T) {
			rec := httptest.NewRecorder()
			url := "/" + page
			if page == "index.html" {
				url = "/" // FileServer 301-redirects /index.html to /
			}
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: %d", url, rec.Code)
			}
			html := rec.Body.String()
			policy := rec.Header().Get("Content-Security-Policy")
			if policy == "" {
				t.Fatal("embedded HTML served without a Content-Security-Policy")
			}
			csp := parseCSP(policy)

			// Hard bans.
			for _, bad := range []string{"'unsafe-eval'", "*", "http:", "https:"} {
				for dir, srcs := range csp {
					if has(srcs, bad) {
						t.Errorf("%s allows %s", dir, bad)
					}
				}
			}
			if has(csp["script-src"], "'unsafe-inline'") {
				t.Error("script-src must not use 'unsafe-inline' (inline scripts are hash-allowed)")
			}
			if !has(csp["default-src"], "'none'") || !has(csp["frame-ancestors"], "'none'") ||
				!has(csp["object-src"], "'none'") || !has(csp["base-uri"], "'none'") {
				t.Errorf("missing deny-by-default directives: %s", policy)
			}

			// Inline scripts must each be hash-allowed.
			inline := 0
			for _, m := range inlineScriptRe.FindAllStringSubmatch(html, -1) {
				if srcAttrRe.MatchString(m[1]) || m[2] == "" {
					continue
				}
				inline++
				sum := sha256.Sum256([]byte(m[2]))
				want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
				if !has(csp["script-src"], want) {
					t.Errorf("inline script %.40q not allowed by script-src (%s)", m[2], want)
				}
			}
			if inline == 0 && !strings.HasSuffix(page, "404.html") {
				t.Logf("note: %s has no inline scripts", page)
			}
			if onHandlerRe.MatchString(html) {
				t.Error("inline event-handler attributes are present; the policy forbids them")
			}

			// Every referenced subresource must be same-origin and map to an
			// allowed directive.
			for _, m := range tagRe.FindAllStringSubmatch(html, -1) {
				tag := strings.ToLower(m[1])
				attrs := map[string]string{}
				for _, a := range attrRe.FindAllStringSubmatch(m[2], -1) {
					attrs[strings.ToLower(a[1])] = a[2]
				}
				switch tag {
				case "script":
					if src, ok := attrs["src"]; ok {
						if !sameOriginPath(src) || !has(csp["script-src"], "'self'") {
							t.Errorf("script src %q not permitted", src)
						}
					}
				case "link":
					href := attrs["href"]
					if href == "" {
						continue
					}
					if !sameOriginPath(href) {
						t.Errorf("link href %q is not same-origin", href)
						continue
					}
					dir := "img-src"
					switch {
					case strings.Contains(attrs["rel"], "stylesheet"):
						dir = "style-src"
					case attrs["as"] == "font":
						dir = "font-src"
					case attrs["as"] == "script":
						dir = "script-src"
					case attrs["as"] == "style":
						dir = "style-src"
					case strings.Contains(attrs["rel"], "manifest"):
						dir = "manifest-src"
					}
					if !has(csp[dir], "'self'") {
						t.Errorf("link %q needs %s 'self'", href, dir)
					}
				case "img":
					if src := attrs["src"]; src != "" && !strings.HasPrefix(src, "data:") && !sameOriginPath(src) {
						t.Errorf("img src %q not same-origin", src)
					}
				case "iframe", "object", "embed", "base":
					t.Errorf("<%s> is forbidden by the policy but present", tag)
				case "form":
					if a := attrs["action"]; a != "" && !sameOriginPath(a) {
						t.Errorf("form action %q not same-origin", a)
					}
				}
			}
			// Inline <style> and style="" need style-src 'unsafe-inline'.
			if (strings.Contains(html, "<style") || strings.Contains(html, ` style="`)) &&
				!has(csp["style-src"], "'unsafe-inline'") {
				t.Error("page uses inline styles but style-src lacks 'unsafe-inline'")
			}
		})
	}

	// Stylesheets: every url() must be a same-origin font/image.
	_ = fs.WalkDir(fsys, "_next/static/css", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".css") {
			return nil
		}
		data, _ := fs.ReadFile(fsys, p)
		for _, m := range cssURLRe.FindAllStringSubmatch(string(data), -1) {
			ref := strings.Trim(m[1], `"' `)
			switch {
			case strings.HasPrefix(ref, "data:"):
				// img-src allows data:; fonts via data: would not be (font-src 'self').
				if strings.Contains(ref, "font") {
					t.Errorf("%s embeds a data: font, forbidden by font-src", p)
				}
			case !sameOriginPath(ref):
				t.Errorf("%s references non-same-origin url(%s)", p, ref)
			case strings.HasSuffix(ref, ".woff2"), strings.HasSuffix(ref, ".woff"):
				if _, err := fs.Stat(fsys, strings.TrimPrefix(ref, "/")); err != nil {
					t.Errorf("%s references missing font %s", p, ref)
				}
			}
		}
		return nil
	})
}

func TestContentSecurityPolicy_HashesInlineScriptsOnly(t *testing.T) {
	html := []byte(`<html><script src="/a.js"></script><script>alert(1)</script><script type="x">b</script></html>`)
	p := ContentSecurityPolicy(html)
	sum := sha256.Sum256([]byte("alert(1)"))
	if !strings.Contains(p, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
		t.Errorf("inline hash missing: %s", p)
	}
	if strings.Count(p, "sha256-") != 2 {
		t.Errorf("expected 2 inline hashes (src script excluded): %s", p)
	}
	if strings.Contains(p, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("script-src must not be unsafe-inline: %s", p)
	}
}

func TestHandler_CSPOnHTMLAndNotOnAssets_404Semantics(t *testing.T) {
	fsys, err := Assets()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(fsys)
	for _, path := range []string{"/", "/index.html", "/dashboard", "/login", "/some/spa/route"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") == "" {
			if path == "/index.html" && rec.Code == http.StatusMovedPermanently {
				continue // FileServer canonical redirect
			}
			t.Errorf("%s: code=%d csp=%q", path, rec.Code, rec.Header().Get("Content-Security-Policy"))
		}
	}
	// A missing asset is still a real 404 (not the SPA shell), with no document CSP.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_next/static/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing asset: want 404 got %d", rec.Code)
	}
}
