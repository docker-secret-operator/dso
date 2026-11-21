// Package backend is the parent directory of DSO's in-process secret backends,
// pkg/backend/env and pkg/backend/file.
//
// Status (G024 disposition, docs/design/g024-disposition.md): this top-level
// package intentionally contains no production code; it holds only
// watch_firstdelivery_test.go. The subpackages are ACTIVE: they are imported
// by pkg/provider (load.go) as the built-in "env" and "file" providers. The
// package lives under pkg/ and is importable by path, so it is retained for
// public-compatibility reasons and must not be removed solely because it has
// no importers.
package backend
