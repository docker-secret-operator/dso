package webui

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// indexFile is the SPA fallback target. Any request that doesn't map to a
// real embedded file (directly, or via the Next.js static-export "route.html"
// convention below) is served this file instead, so that a client-side
// router can handle deep links.
const indexFile = "index.html"

// Handler serves the embedded WebUI static assets with SPA fallback
// semantics: a request for a real file under assets/ is served directly; any
// other request falls back to index.html. It is a plain http.Handler meant
// to be mounted directly onto the existing REST mux for non-/api, non-/health
// paths -- see internal/server/rest.go.
type Handler struct {
	fsys       fs.FS
	fileServer http.Handler
}

// NewHandler builds a Handler over the embedded (or, in tests, injected)
// asset filesystem.
func NewHandler(fsys fs.FS) *Handler {
	return &Handler{
		fsys:       fsys,
		fileServer: http.FileServer(http.FS(fsys)),
	}
}

// ServeHTTP implements the SPA-fallback static file handler.
//
// Path traversal safety: fsys is an fs.FS obtained via fs.Sub over an
// embed.FS. Both http.FS's Open (via io/fs's ValidPath rules) and fs.Sub
// reject any path containing "..", empty elements, or a leading "/" --
// requests are rejected with fs.ErrInvalid before ever touching the
// underlying embed.FS, so there is no way to escape the embedded tree
// regardless of what a client sends (verified by handler_test.go).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := cleanName(r.URL.Path)

	// Exact match: static assets under _next/, favicon, etc. Let
	// http.FileServer handle these (correct content-type, range, caching).
	if f, err := h.fsys.Open(name); err == nil {
		_ = f.Close()
		h.fileServer.ServeHTTP(w, r)
		return
	}

	// `next export`'s app-router output writes each route to "<route>.html"
	// rather than "<route>/index.html" (e.g. app/dashboard/page.tsx ->
	// dashboard.html). A request for the clean route path ("/dashboard")
	// therefore has no exact file match above; try the ".html" form before
	// giving up and falling back to the SPA shell, so a direct load of
	// /dashboard or /login serves that route's own prerendered HTML (title,
	// meta tags, initial DOM) instead of always re-serving "/"'s markup.
	if name != indexFile && !strings.HasSuffix(name, ".html") {
		if f, err := h.fsys.Open(name + ".html"); err == nil {
			_ = f.Close()
			h.serveFile(w, r, name+".html")
			return
		}
	}

	// No matching embedded file (including the case where the path is
	// invalid/traversal-shaped, since fsys.Open rejects it the same way):
	// fall back to index.html for SPA-style client routing.
	h.serveIndex(w, r)
}

// serveIndex reads and writes index.html's bytes directly, bypassing
// net/http's ServeFile/ServeContent index.html redirect special-case (see
// serveFile below for why that matters here).
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, indexFile)
}

// serveFile reads and writes the named embedded file's bytes directly.
//
// Deliberately NOT using http.ServeFile/ServeFileFS here: those special-case
// any request path ending in "/index.html" by issuing a 301 redirect to
// strip it, which would turn an SPA-fallback or route response into a
// redirect instead of serving content directly.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f, err := h.fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// cleanName cleans the requested path and maps it to an fs.FS-relative name
// (no leading slash; "" -> indexFile), mirroring what http.FileServer does
// internally so callers can probe for a file's existence up front.
func cleanName(reqPath string) string {
	clean := path.Clean("/" + reqPath)
	name := clean[1:] // fs.FS wants no leading slash
	if name == "" || name == "." {
		name = indexFile
	}
	return name
}
