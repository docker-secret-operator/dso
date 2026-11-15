package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// Content-Security-Policy for the embedded Next.js static export.
//
// What the real pages need (verified against internal/webui/assets by
// csp_test.go, which parses every embedded HTML file):
//
//   - scripts: same-origin /_next/static/chunks/*.js plus a handful of INLINE
//     <script> blocks per page (the React Server Components flight payload,
//     self.__next_f.push(...)). A static export cannot use per-response
//     nonces, so instead of 'unsafe-inline' every inline block is allowed by
//     its SHA-256 hash, computed here from the exact embedded bytes. A
//     script injected into a page (XSS) is therefore not executable. There is
//     no 'unsafe-eval'.
//   - styles: same-origin stylesheet plus inline style="" attributes and a
//     <style> block (404.html) and runtime style injection by React/Next. Hash
//     allow-listing cannot cover style attributes without 'unsafe-hashes' and
//     does not cover runtime-injected styles, so style-src keeps
//     'unsafe-inline'. This is the one deliberate relaxation; style injection
//     cannot execute script.
//   - fonts: same-origin /_next/static/media/*.woff2 (font-src 'self').
//   - images: same-origin /icon.svg; data: allowed for inline SVG/CSS images.
//   - connect: same-origin fetch to /api and the same-origin WebSocket
//     (/api/events/ws); 'self' covers ws:/wss: to the same host.
//
// Everything else (frames, objects, base-uri, external origins) is denied.
const policyTemplate = "default-src 'none'; " +
	"script-src 'self'%s; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"form-action 'self'; " +
	"base-uri 'none'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'"

var (
	inlineScriptRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	srcAttrRe      = regexp.MustCompile(`(?i)(^|\s)src\s*=`)
)

// ContentSecurityPolicy returns the policy for one embedded HTML document:
// the fixed directives above plus a 'sha256-...' source for each inline
// <script> block in html.
func ContentSecurityPolicy(html []byte) string {
	seen := map[string]bool{}
	var hashes []string
	for _, m := range inlineScriptRe.FindAllSubmatch(html, -1) {
		if srcAttrRe.Match(m[1]) || len(m[2]) == 0 {
			continue // external script (covered by 'self') or empty
		}
		sum := sha256.Sum256(m[2])
		h := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if !seen[h] {
			seen[h] = true
			hashes = append(hashes, h)
		}
	}
	sort.Strings(hashes)
	extra := ""
	if len(hashes) > 0 {
		extra = " " + strings.Join(hashes, " ")
	}
	return strings.Replace(policyTemplate, "%s", extra, 1)
}

// buildCSPIndex computes the per-document policy for every .html file in fsys.
func buildCSPIndex(fsys fs.FS) map[string]string {
	idx := map[string]string{}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		if data, rerr := fs.ReadFile(fsys, p); rerr == nil {
			idx[p] = ContentSecurityPolicy(data)
		}
		return nil
	})
	return idx
}
