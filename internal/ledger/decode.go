package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// This file implements the two-phase strict decoder of design §36.2 and the
// field presence policy of §29.1.

// Reason classes used in decoding errors (design §29.4). A message names a field
// path and one of these classes and never echoes document content.
const (
	classSyntax      = "syntax error"
	classType        = "type error"
	classUnknown     = "unknown field"
	classDuplicate   = "duplicate key"
	classCaseVariant = "case variant of a field name"
	classMissing     = "missing field"
	classValue       = "invalid value"
)

var integerLiteral = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

var timeType = reflect.TypeOf(time.Time{})

// decodeErr builds an ErrInvalid error for the field at path (the document root
// when path is empty) with a reason class and an optional detail that never
// contains document content.
func decodeErr(path, class, detail string) error {
	if path == "" {
		path = "document"
	}
	if detail != "" {
		return invalid(path, "%s: %s", class, detail)
	}
	return invalid(path, "%s", class)
}

// syntaxErr describes a JSON syntax problem by byte offset only.
func syntaxErr(err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return decodeErr("", classSyntax, fmt.Sprintf("at byte %d", syn.Offset))
	}
	return decodeErr("", classSyntax, "")
}

// checkEnvelope is phase 1 (design §36.2, P1-1 to P1-8). It returns nil only
// for a syntactically valid object whose single top-level schema_version is the
// supported version; a greater version returns *SchemaVersionError and
// nothing else about the document is examined.
func checkEnvelope(data []byte) error {
	if !utf8.Valid(data) {
		return decodeErr("", classSyntax, "input is not valid UTF-8")
	}
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		return decodeErr("", classSyntax, "byte-order mark")
	}
	// Syntax of the whole input, including any trailing data. Nested content is
	// checked for syntax only: names, duplicates and types are phase 2.
	var whole json.RawMessage
	if err := json.Unmarshal(data, &whole); err != nil {
		return syntaxErr(err)
	}
	if first := bytes.TrimLeft(data, " \t\r\n"); len(first) == 0 || first[0] != '{' {
		return decodeErr("", classType, "the document is not a JSON object")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if _, err := dec.Token(); err != nil { // the opening brace
		return syntaxErr(err)
	}
	var versions []json.RawMessage
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return syntaxErr(err)
		}
		key, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return syntaxErr(err)
		}
		if key == "schema_version" {
			versions = append(versions, raw)
		}
	}
	switch len(versions) {
	case 0:
		return decodeErr("schema_version", classMissing, "")
	case 1:
	default:
		return decodeErr("schema_version", classDuplicate, "")
	}
	lit := strings.TrimSpace(string(versions[0]))
	if !integerLiteral.MatchString(lit) {
		return decodeErr("schema_version", classType, "must be a JSON integer")
	}
	n, err := strconv.ParseInt(lit, 10, 64)
	if err != nil {
		// An integer literal beyond 64 bits: a positive one is greater than any
		// supported version, a negative one is below it.
		if strings.HasPrefix(lit, "-") {
			return decodeErr("schema_version", classValue, "unsupported schema version")
		}
		return &SchemaVersionError{Found: math.MaxInt, Supported: SchemaVersion}
	}
	switch {
	case n > SchemaVersion:
		found := math.MaxInt
		if n <= math.MaxInt {
			found = int(n)
		}
		return &SchemaVersionError{Found: found, Supported: SchemaVersion}
	case n < SchemaVersion:
		return decodeErr("schema_version", classValue, fmt.Sprintf("unsupported schema version %d (this package reads %d)", n, SchemaVersion))
	}
	return nil
}

// decodeStrict is phase 2 up to and including decoding (P2-1 to P2-5): it
// scans the document against the types of the schema, then decodes it.
func decodeStrict(data []byte, l *Ledger) error {
	if err := checkDepth(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := scanValue(dec, reflect.TypeOf(Ledger{}), "", false); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return decodeErr("", classSyntax, "unexpected data after the ledger object")
	}
	// The scan has established exact names, no duplicates, no unknown fields and
	// the right JSON kinds, so this decode cannot disagree with it; unknown
	// fields stay rejected as a second line of defence.
	d2 := json.NewDecoder(bytes.NewReader(data))
	d2.DisallowUnknownFields()
	if err := d2.Decode(l); err != nil {
		return decodeErr("", classType, "the document does not match the schema")
	}
	return nil
}

// MaxJSONDepth is the deepest JSON nesting a ledger document may have, with the
// document object itself at depth 1 (owner decision M-S2-1). It is checked only
// in phase 2, so a newer schema version keeps precedence over it.
const MaxJSONDepth = 32

// checkDepth rejects a document nested deeper than MaxJSONDepth. Phase 1 has
// already established that the input is syntactically valid JSON, so a token
// error here cannot occur and is reported as a syntax error anyway.
func checkDepth(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return syntaxErr(err)
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
				if depth > MaxJSONDepth {
					return decodeErr("", classValue, fmt.Sprintf("nesting is deeper than %d levels", MaxJSONDepth))
				}
			default:
				depth--
			}
		}
	}
}

type fieldInfo struct {
	name     string
	typ      reflect.Type
	optional bool // omitzero: absent when unset
}

var (
	fieldCacheMu sync.Mutex
	fieldCache   = map[reflect.Type][]fieldInfo{}
)

func structFields(t reflect.Type) []fieldInfo {
	fieldCacheMu.Lock()
	defer fieldCacheMu.Unlock()
	if fs, ok := fieldCache[t]; ok {
		return fs
	}
	var fs []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		fs = append(fs, fieldInfo{name: name, typ: f.Type, optional: strings.Contains(","+opts+",", ",omitzero,")})
	}
	fieldCache[t] = fs
	return fs
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// scanValue consumes one JSON value from dec and checks it against type t.
// optional marks a field that may be absent when unset (omitzero), for which
// null is also tolerated.
func scanValue(dec *json.Decoder, t reflect.Type, path string, optional bool) error {
	tok, err := dec.Token()
	if err != nil {
		return syntaxErr(err)
	}
	switch v := tok.(type) {
	case nil:
		if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || optional {
			return nil
		}
		return decodeErr(path, classType, "null is not allowed here")
	case json.Delim:
		switch v {
		case '{':
			base := t
			if base.Kind() == reflect.Pointer {
				base = base.Elem()
			}
			if base.Kind() != reflect.Struct || base == timeType {
				return decodeErr(path, classType, "unexpected object")
			}
			return scanObject(dec, base, path)
		case '[':
			if t.Kind() != reflect.Slice {
				return decodeErr(path, classType, "unexpected array")
			}
			for i := 0; dec.More(); i++ {
				if err := scanValue(dec, t.Elem(), fmt.Sprintf("%s[%d]", path, i), false); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return syntaxErr(err)
			}
			return nil
		}
		return decodeErr(path, classSyntax, "unexpected delimiter")
	case string:
		switch {
		case t == timeType:
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return decodeErr(path, classValue, "not an RFC 3339 timestamp")
			}
			return nil
		case t.Kind() == reflect.String:
			return nil
		}
		return decodeErr(path, classType, "unexpected string")
	case json.Number:
		lit := v.String()
		if !integerLiteral.MatchString(lit) {
			return decodeErr(path, classType, "must be a JSON integer")
		}
		switch t.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if _, err := strconv.ParseInt(lit, 10, t.Bits()); err != nil {
				return decodeErr(path, classValue, "integer out of range")
			}
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if _, err := strconv.ParseUint(lit, 10, t.Bits()); err != nil {
				return decodeErr(path, classValue, "must be a non-negative integer in range")
			}
			return nil
		}
		return decodeErr(path, classType, "unexpected number")
	}
	return decodeErr(path, classType, "unexpected value")
}

// scanObject consumes the rest of an object whose opening brace has been read.
func scanObject(dec *json.Decoder, st reflect.Type, path string) error {
	fields := structFields(st)
	byName := make(map[string]fieldInfo, len(fields))
	for _, f := range fields {
		byName[f.name] = f
	}
	seen := make(map[string]bool, len(fields))
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return syntaxErr(err)
		}
		key, _ := kt.(string)
		fi, ok := byName[key]
		if !ok {
			for _, f := range fields {
				if strings.EqualFold(f.name, key) {
					return decodeErr(path, classCaseVariant, "")
				}
			}
			return decodeErr(path, classUnknown, "")
		}
		if seen[key] {
			return decodeErr(path, classDuplicate, "")
		}
		seen[key] = true
		if err := scanValue(dec, fi.typ, joinPath(path, key), fi.optional); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return syntaxErr(err)
	}
	for _, f := range fields {
		if !f.optional && !seen[f.name] {
			return decodeErr(joinPath(path, f.name), classMissing, "")
		}
	}
	return nil
}
