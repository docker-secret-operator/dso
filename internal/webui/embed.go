// Package webui serves the DSO browser dashboard's static assets from the
// same HTTP listener as the REST API (internal/server). It intentionally
// does not run a second HTTP server or a reverse proxy: the whole point of
// this package, given DSO's single-binary/single-process architecture, is
// to be a plain http.Handler that internal/server mounts directly onto its
// existing mux for any path that isn't /api/* or /health.
//
// The assets/ directory embedded here is currently a placeholder -- see
// assets/index.html for why. Porting the real Next.js static export into
// assets/ is tracked as a separate step and is explicitly out of scope for
// this package's initial version.
package webui

import (
	"embed"
	"io/fs"
)

// rawAssets contains the embedded WebUI static assets. Built (eventually)
// from `npm run build` in web/, with output copied to internal/webui/assets/.
// As of this version, assets/ contains only a placeholder index.html so the
// serving mechanism (embed -> mux -> SPA fallback) can be built and tested
// ahead of the real frontend port.
//
//go:embed assets/*
var rawAssets embed.FS

// Assets returns the embedded filesystem rooted at assets/, suitable for
// http.FS / http.FileServer use.
func Assets() (fs.FS, error) {
	return fs.Sub(rawAssets, "assets")
}
