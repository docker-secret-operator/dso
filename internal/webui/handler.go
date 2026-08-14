package webui

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"time"
)

// indexFile is the SPA fallback target. Any request that doesn't map to a
// real embedded file is served this file instead, so that a client-side
// router (once one exists) can handle deep links.
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
	if f, err := h.open(r.URL.Path); err != nil {
		// No matching embedded file (including the case where the path is
		// invalid/traversal-shaped, since fsys.Open rejects it the same
		// way): fall back to index.html for SPA-style client routing.
		//
		// Deliberately NOT using http.ServeFile/ServeFileFS here: those
		// special-case any request path ending in "/index.html" by issuing a
		// 301 redirect to strip it, which would turn every SPA-fallback
		// response into a redirect instead of serving content directly.
		h.serveIndex(w, r)
		return
	} else {
		_ = f.Close()
	}
	h.fileServer.ServeHTTP(w, r)
}

// serveIndex reads and writes index.html's bytes directly, bypassing
// net/http's ServeFile/ServeContent index.html redirect special-case (see
// ServeHTTP above for why that matters here).
func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := h.fsys.Open(indexFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// open cleans and opens the requested path against fsys, mirroring what
// http.FileServer does internally, so we can decide up front whether to
// fall back to index.html.
func (h *Handler) open(reqPath string) (fs.File, error) {
	clean := path.Clean("/" + reqPath)
	name := clean[1:] // fs.FS wants no leading slash
	if name == "" {
		name = "."
	}
	if name == "." {
		name = indexFile
	}
	return h.fsys.Open(name)
}
