package util

// ShortID returns up to the first 12 characters of id.
// Safe for empty or short strings — never panics.
func ShortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
