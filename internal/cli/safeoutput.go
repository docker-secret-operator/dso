package cli

import (
	"strconv"
	"strings"
)

// quoteUntrusted renders a database-derived value (for example an audit event
// id) for human-readable output: it is double-quoted with every control and
// otherwise non-printable character escaped (\n, \r, \x1b, ‮, ...), so
// it can neither start a new output line nor inject terminal control
// sequences, and stays unambiguous and useful for identifying the event.
func quoteUntrusted(s string) string { return strconv.Quote(s) }

// printableUntrusted makes free text that may embed untrusted or
// operator-supplied content (such as an underlying error) safe for a single
// output line: printable characters are kept as they are, and every control or
// otherwise non-printable character is replaced by an ASCII escape.
func printableUntrusted(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strconv.IsPrint(r) {
			b.WriteRune(r)
			continue
		}
		q := strconv.QuoteRuneToASCII(r) // 'x' with escapes
		b.WriteString(q[1 : len(q)-1])
	}
	return b.String()
}

// escapeJSONNonPrintable rewrites the output of encoding/json so that
// non-printable characters inside strings that the encoder leaves raw (for
// example the bidirectional override U+202E) are written as \uXXXX escapes.
// The document still decodes to exactly the same values. Only a JSON string
// can contain such characters here: the encoder's own structural whitespace is
// just '\n' and ' ', which are kept.
func escapeJSONNonPrintable(b []byte) []byte {
	var out strings.Builder
	for _, r := range string(b) {
		if r == '\n' || strconv.IsPrint(r) {
			out.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			r -= 0x10000
			out.WriteString(`\u` + hex4(0xD800+(r>>10)) + `\u` + hex4(0xDC00+(r&0x3FF)))
			continue
		}
		out.WriteString(`\u` + hex4(r))
	}
	return []byte(out.String())
}

func hex4(r rune) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[(r>>12)&0xF], digits[(r>>8)&0xF], digits[(r>>4)&0xF], digits[r&0xF]})
}
