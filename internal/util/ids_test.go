package util

import "testing"

func TestShortID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"123456789012", "123456789012"},        // exactly 12 — returned as-is
		{"1234567890123", "123456789012"},        // 13 chars — truncated to 12
		{"abcdefghijklmnopqrstuvwxyz", "abcdefghijkl"}, // long ID
	}
	for _, tc := range cases {
		got := ShortID(tc.in)
		if got != tc.want {
			t.Errorf("ShortID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShortID_NeverPanics(t *testing.T) {
	// Verify boundary: length 11, 12, 13 all safe
	for _, n := range []int{0, 1, 11, 12, 13, 100} {
		id := make([]byte, n)
		for i := range id {
			id[i] = 'x'
		}
		got := ShortID(string(id))
		if len(got) > 12 {
			t.Errorf("ShortID returned %d chars for input len %d", len(got), n)
		}
	}
}
