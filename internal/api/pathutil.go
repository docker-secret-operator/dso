package api

import "strings"

// resourceIDFromPath extracts the {id} segment from REST paths of the form
// /api/<resource>/<id> or /api/<resource>/<id>/<action>.
//
// These handlers are dispatched through manual path switches (not a Go ServeMux
// with {id} patterns), so http.Request.PathValue("id") is always empty here —
// the id must be parsed from the path directly.
func resourceIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// ["api", "<resource>", "<id>", ...]
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}
