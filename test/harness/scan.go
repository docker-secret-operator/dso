package harness

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Finding is one occurrence of the synthetic secret sentinel in a place it must
// never appear. It deliberately carries NO surrounding text: a finding must not
// itself leak the value into test output.
type Finding struct {
	Path    string
	Variant string // plain, hex, base64, url
	Offset  int64
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: sentinel (%s form) at byte %d", f.Path, f.Variant, f.Offset)
}

// sentinelVariants returns the encodings under which a leaked value might appear.
func sentinelVariants(sentinel string) map[string][]byte {
	v := map[string][]byte{
		"plain":  []byte(sentinel),
		"hex":    []byte(hex.EncodeToString([]byte(sentinel))),
		"url":    []byte(url.QueryEscape(sentinel)),
		"base64": []byte(base64.StdEncoding.EncodeToString([]byte(sentinel))),
	}
	// base64 of a substring depends on its alignment: include all three offsets.
	for i := 1; i <= 2; i++ {
		enc := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{'x'}, i), sentinel...))
		// drop the chars that encode the padding bytes (first 2 chars per prefix byte pair)
		cut := (i*8 + 5) / 6
		if len(enc) > cut+4 {
			v[fmt.Sprintf("base64+%d", i)] = []byte(strings.TrimRight(enc[cut+1:len(enc)-2], "="))
		}
	}
	return v
}

// ScanReader reports sentinel occurrences in r (any variant).
func ScanReader(r io.Reader, sentinel, name string) ([]Finding, error) {
	if len(sentinel) < 8 {
		return nil, fmt.Errorf("sentinel %q is too short to scan for reliably", name)
	}
	data, err := io.ReadAll(io.LimitReader(r, 256<<20))
	if err != nil {
		return nil, err
	}
	var out []Finding
	for kind, pat := range sentinelVariants(sentinel) {
		if len(pat) < 8 {
			continue
		}
		if i := bytes.Index(data, pat); i >= 0 {
			out = append(out, Finding{Path: name, Variant: kind, Offset: int64(i)})
		}
	}
	return out, nil
}

// ScanTree walks root (without following symlinks) and scans every regular file
// not rejected by skip. skip may be nil. A file that cannot be read is an error:
// an unscanned file is not evidence of absence.
func ScanTree(root, sentinel string, skip func(path string) bool) ([]Finding, error) {
	var all []Finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || (skip != nil && skip(path)) {
			return nil
		}
		f, err := os.Open(path) // #nosec G304 -- harness-owned directory
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		fs, err := ScanReader(f, sentinel, path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		all = append(all, fs...)
		return nil
	})
	return all, err
}

// ScanFiles scans the given regular files (missing files are an error).
func ScanFiles(sentinel string, paths ...string) ([]Finding, error) {
	var all []Finding
	for _, p := range paths {
		f, err := os.Open(p) // #nosec G304 -- harness-owned path
		if err != nil {
			return nil, err
		}
		fs, err := ScanReader(f, sentinel, p)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		all = append(all, fs...)
	}
	return all, nil
}
