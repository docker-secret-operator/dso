// Package webui serves the DSO browser dashboard's static assets from the
// same HTTP listener as the REST API (internal/server). It intentionally
// does not run a second HTTP server or a reverse proxy: the whole point of
// this package, given DSO's single-binary/single-process architecture, is
// to be a plain http.Handler that internal/server mounts directly onto its
// existing mux for any path that isn't /api/* or /health.
//
// The assets/ directory embedded here is the production static export of
// the Next.js dashboard whose source lives in web/. It is generated, not
// hand-written: run `make webui-build` (or scripts/build-webui.sh directly)
// to rebuild it from web/ via `npm ci` + `npm run build` (web/next.config.js
// sets `output: 'export'`), and commit the resulting diff in assets/ along
// with the web/ source change that produced it. assets/ is intentionally
// tracked in git (see the note next to the web/node_modules|.next|out
// exclusions in .gitignore) so the binary can be built with plain `go
// build` and no Node toolchain. The "webui" job in
// .github/workflows/ci.yml regenerates assets/ fresh on every push/PR and
// fails the build if it differs from what's committed, so this directory
// cannot silently drift out of sync with web/. See docs/webui-build.md for
// the full source -> build -> embed flow.
package webui

import (
	"embed"
	"io/fs"
)

// rawAssets contains the embedded WebUI static assets, built from web/ via
// `make webui-build` (scripts/build-webui.sh) and copied into assets/.
//
//go:embed assets/*
var rawAssets embed.FS

// Assets returns the embedded filesystem rooted at assets/, suitable for
// http.FS / http.FileServer use.
func Assets() (fs.FS, error) {
	return fs.Sub(rawAssets, "assets")
}
