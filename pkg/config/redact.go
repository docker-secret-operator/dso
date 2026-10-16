package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RedactedMarker replaces every credential value in redacted output.
const RedactedMarker = "[REDACTED]"

// credentialKeyPattern is the BACKSTOP for keys the schema does not know
// (a newer config, a typo such as "hmac_secrt", a provider-specific map). It is
// never applied to keys the schema DOES define: those are governed solely by
// their `dso` tag, so ordinary fields such as `secrets:` (the mapping list) or
// `session_idle_timeout` are not mangled.
var credentialKeyPattern = regexp.MustCompile(`(?i)(secret|token|passw(or)?d|credential|api[-_]?key|access[-_]?key|private[-_]?key|hmac|bearer|dsn)`)

type redactKind int

const (
	kindSecret      redactKind = iota + 1 // replace every scalar leaf
	kindURL                               // keep scheme://host, drop userinfo/path/query
	kindSecretByKey                       // map: redact entries whose key looks like a credential
)

func parseDSOTag(tag string) (redactKind, bool) {
	switch tag {
	case "secret":
		return kindSecret, true
	case "url":
		return kindURL, true
	case "secret-by-key":
		return kindSecretByKey, true
	}
	return 0, false
}

type redactor struct {
	targets  map[*yaml.Node]redactKind
	order    []*yaml.Node
	needTree bool // set when an in-place text edit is not provably safe
}

// RedactYAML returns data with every credential value replaced by
// RedactedMarker, for display. It is schema-aware: it walks the YAML together
// with the Config type and redacts exactly the fields tagged `dso:"..."`,
// plus a name-based backstop for keys the schema does not define.
//
// Fidelity: when every credential is a single-line scalar, only those scalars'
// bytes change (comments, blank lines, ordering and indentation of everything
// else are preserved byte-for-byte); each replacement is verified by
// re-parsing the original token. Otherwise (block scalars, flow collections,
// aliases, anchors, multi-document files) the document is re-encoded with
// credentials replaced, which preserves content and comments but may change
// layout.
//
// Safety: it fails closed. Input that is not valid YAML returns an error (the
// caller must not print the raw bytes), and a final scrub removes any remaining
// copy of a redacted value of 8+ characters anywhere in the output, including
// comments.
func RedactYAML(data []byte) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("config is not valid YAML: %w", err)
		}
		docs = append(docs, &n)
	}
	if len(docs) == 0 {
		return data, nil
	}

	r := &redactor{targets: map[*yaml.Node]redactKind{}}
	for _, d := range docs {
		r.walk(d, reflect.TypeOf(Config{}), false)
	}
	if len(r.order) == 0 {
		return data, nil
	}

	// Remember every original credential value for the final scrub.
	var secrets []string
	for _, n := range r.order {
		if n.Kind == yaml.ScalarNode && n.Value != "" {
			secrets = append(secrets, n.Value)
			if r.targets[n] == kindURL {
				if u, err := url.Parse(n.Value); err == nil {
					secrets = append(secrets, strings.Trim(u.EscapedPath(), "/"), u.RawQuery)
					if u.User != nil {
						if pw, ok := u.User.Password(); ok {
							secrets = append(secrets, pw)
						}
					}
				}
			}
		}
	}

	var out []byte
	if len(docs) == 1 && !r.needTree {
		if edited, ok := r.editInPlace(data); ok {
			out = edited
		}
	}
	if out == nil {
		for _, n := range r.order {
			r.rewriteNode(n)
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		for _, d := range docs {
			if err := enc.Encode(d); err != nil {
				return nil, fmt.Errorf("render redacted config: %w", err)
			}
		}
		if err := enc.Close(); err != nil {
			return nil, fmt.Errorf("render redacted config: %w", err)
		}
		out = buf.Bytes()
	}
	return scrub(out, secrets), nil
}

// walk descends n alongside schema type t, registering credential nodes.
func (r *redactor) walk(n *yaml.Node, t reflect.Type, inFlow bool) {
	if n == nil {
		return
	}
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	flow := inFlow || n.Style&yaml.FlowStyle != 0
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			r.walk(c, t, flow)
		}
	case yaml.AliasNode:
		r.walk(n.Alias, t, flow)
	case yaml.MappingNode:
		switch {
		case t != nil && t.Kind() == reflect.Struct:
			r.walkStruct(n, t, flow)
		case t != nil && t.Kind() == reflect.Map:
			for i := 0; i+1 < len(n.Content); i += 2 {
				r.walk(n.Content[i+1], t.Elem(), flow)
			}
		default:
			r.walkUnknown(n, flow)
		}
	case yaml.SequenceNode:
		var elem reflect.Type
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			elem = t.Elem()
		}
		for _, c := range n.Content {
			r.walk(c, elem, flow)
		}
	}
}

func (r *redactor) walkStruct(n *yaml.Node, t reflect.Type, flow bool) {
	fields := yamlFields(t)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		f, known := fields[k.Value]
		if !known {
			if credentialKeyPattern.MatchString(k.Value) {
				r.markAll(v, kindSecret, flow)
			} else {
				r.walkUnknown(v, flow)
			}
			continue
		}
		if kind, tagged := parseDSOTag(f.Tag.Get("dso")); tagged {
			if kind == kindSecretByKey {
				r.markByKey(v, flow)
			} else {
				r.markAll(v, kind, flow)
			}
			continue
		}
		r.walk(v, f.Type, flow)
	}
}

// walkUnknown applies the name-based backstop to a subtree the schema does not
// describe.
func (r *redactor) walkUnknown(n *yaml.Node, inFlow bool) {
	if n == nil {
		return
	}
	flow := inFlow || n.Style&yaml.FlowStyle != 0
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			r.walkUnknown(c, flow)
		}
	case yaml.AliasNode:
		r.walkUnknown(n.Alias, flow)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if credentialKeyPattern.MatchString(n.Content[i].Value) {
				r.markAll(n.Content[i+1], kindSecret, flow)
			} else {
				r.walkUnknown(n.Content[i+1], flow)
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			r.walkUnknown(c, flow)
		}
	}
}

// markByKey redacts the values of a map whose KEY looks like a credential.
func (r *redactor) markByKey(n *yaml.Node, inFlow bool) {
	flow := inFlow || n.Style&yaml.FlowStyle != 0
	if n.Kind == yaml.AliasNode {
		r.markByKey(n.Alias, flow)
		return
	}
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if credentialKeyPattern.MatchString(n.Content[i].Value) {
			r.markAll(n.Content[i+1], kindSecret, flow)
		}
	}
}

// markAll registers every scalar under n as a credential.
func (r *redactor) markAll(n *yaml.Node, kind redactKind, inFlow bool) {
	if n == nil {
		return
	}
	flow := inFlow || n.Style&yaml.FlowStyle != 0
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" || n.Tag == "!!null" || alreadyRedacted(kind, n.Value) {
			return // unset stays visibly unset; redacted output stays stable
		}
		if _, dup := r.targets[n]; !dup {
			r.targets[n] = kind
			r.order = append(r.order, n)
		}
		if flow || n.Anchor != "" || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 {
			r.needTree = true
		}
	case yaml.AliasNode:
		// An alias hides the value in another node; render it as a literal
		// marker instead of expanding it, and redact the anchored definition
		// it points at too (it may sit under an unrelated, non-sensitive key).
		if _, dup := r.targets[n]; !dup {
			r.targets[n] = kind
			r.order = append(r.order, n)
		}
		r.needTree = true
		r.markAll(n.Alias, kind, flow)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			r.markAll(n.Content[i+1], kind, flow)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			r.markAll(c, kind, flow)
		}
	}
}

// yamlFields indexes the exported fields of struct type t by YAML key,
// flattening `,inline` embedded structs.
func yamlFields(t reflect.Type) map[string]reflect.StructField {
	out := map[string]reflect.StructField{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if strings.Contains(opts, "inline") {
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range yamlFields(ft) {
					out[k] = v
				}
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = f
	}
	return out
}

var redactedURLPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://[^/@\s]+/` + regexp.QuoteMeta(RedactedMarker) + `$`)

// alreadyRedacted recognizes EXACTLY the strings this package produces, so
// redacting twice is a no-op. It is deliberately an exact match: a real
// credential that merely contains the marker text is still redacted.
func alreadyRedacted(kind redactKind, v string) bool {
	if v == RedactedMarker {
		return true
	}
	return kind == kindURL && redactedURLPattern.MatchString(v)
}

// redactedValue is what a credential scalar is replaced with.
func redactedValue(kind redactKind, original string) string {
	if kind == kindURL {
		u, err := url.Parse(original)
		if err != nil || u.Host == "" {
			return RedactedMarker
		}
		clean := u.Scheme + "://" + u.Hostname()
		if p := u.Port(); p != "" {
			clean += ":" + p
		}
		if u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
			clean += "/" + RedactedMarker
		}
		return clean
	}
	return RedactedMarker
}

// rewriteNode turns a registered credential node into a redacted scalar (tree
// re-encode path).
func (r *redactor) rewriteNode(n *yaml.Node) {
	kind := r.targets[n]
	original := n.Value
	n.Kind = yaml.ScalarNode
	n.Alias = nil
	n.Content = nil
	n.Tag = "!!str"
	n.Style = yaml.DoubleQuotedStyle
	n.Value = redactedValue(kind, original)
}

// editInPlace replaces just the credential scalars' bytes. It returns false
// (so the caller re-encodes the tree) unless every replacement is provably
// correct: the raw token located at the node's position must re-parse to the
// node's exact value.
func (r *redactor) editInPlace(data []byte) ([]byte, bool) {
	type edit struct {
		line, start, end int // rune offsets within the line
		repl             string
	}
	nl := "\n"
	lines := strings.Split(string(data), nl)
	var edits []edit
	for _, n := range r.order {
		if n.Kind != yaml.ScalarNode {
			return nil, false
		}
		if n.Line < 1 || n.Line > len(lines) {
			return nil, false
		}
		start, end, ok := scalarSpan([]rune(lines[n.Line-1]), n)
		if !ok {
			return nil, false
		}
		repl := redactedValue(r.targets[n], n.Value)
		edits = append(edits, edit{n.Line - 1, start, end, `"` + repl + `"`})
	}
	// Apply right-to-left within each line so earlier offsets stay valid.
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].line != edits[j].line {
			return edits[i].line < edits[j].line
		}
		return edits[i].start > edits[j].start
	})
	for _, e := range edits {
		rs := []rune(lines[e.line])
		lines[e.line] = string(rs[:e.start]) + e.repl + string(rs[e.end:])
	}
	return []byte(strings.Join(lines, nl)), true
}

// scalarSpan locates the raw token of single-line scalar n within line and
// verifies it re-parses to n.Value.
func scalarSpan(line []rune, n *yaml.Node) (start, end int, ok bool) {
	start = n.Column - 1
	if start < 0 || start >= len(line) {
		return 0, 0, false
	}
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if line[start] != '"' {
			return 0, 0, false
		}
		i := start + 1
		for i < len(line) {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == '"' {
				end = i + 1
				break
			}
			i++
		}
		if end == 0 {
			return 0, 0, false // closing quote is on another line
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		if line[start] != '\'' {
			return 0, 0, false
		}
		i := start + 1
		for i < len(line) {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i += 2
					continue
				}
				end = i + 1
				break
			}
			i++
		}
		if end == 0 {
			return 0, 0, false
		}
	case n.Style == 0:
		end = len(line)
		for i := start; i < len(line); i++ {
			if line[i] == '#' && i > start && (line[i-1] == ' ' || line[i-1] == '\t') {
				end = i
				break
			}
		}
		for end > start && (line[end-1] == ' ' || line[end-1] == '\t' || line[end-1] == '\r') {
			end--
		}
	default:
		return 0, 0, false
	}
	if end <= start {
		return 0, 0, false
	}
	// Verify: the raw token must parse back to exactly the node's value.
	var probe yaml.Node
	if err := yaml.Unmarshal([]byte("k: "+string(line[start:end])), &probe); err != nil {
		return 0, 0, false
	}
	if len(probe.Content) == 0 || len(probe.Content[0].Content) < 2 {
		return 0, 0, false
	}
	if probe.Content[0].Content[1].Value != n.Value {
		return 0, 0, false
	}
	return start, end, true
}

// scrub removes any remaining copy of a redacted value (8+ chars) from the
// output, e.g. one repeated in a comment or under an unrelated key.
func scrub(out []byte, secrets []string) []byte {
	seen := map[string]bool{}
	var uniq []string
	for _, s := range secrets {
		if len(s) >= 8 && !seen[s] {
			seen[s] = true
			uniq = append(uniq, s)
		}
	}
	sort.Slice(uniq, func(i, j int) bool { return len(uniq[i]) > len(uniq[j]) })
	for _, s := range uniq {
		out = bytes.ReplaceAll(out, []byte(s), []byte(RedactedMarker))
	}
	return out
}
