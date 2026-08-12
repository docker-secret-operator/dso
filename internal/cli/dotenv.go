package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// dotEnvMaxLineSize bounds a single .env line/value, matching the existing
// `docker dso env import` limit.
const dotEnvMaxLineSize = 1024 * 1024

// parseDotEnvResult is the outcome of parsing a .env-format file.
type parseDotEnvResult struct {
	// Values holds the last value seen for each key, in .env parsing
	// order -- i.e. later duplicate assignments win, matching shell .env
	// semantics and the pre-existing `env import` behavior.
	Values map[string]string
	// DuplicateKeys lists keys that appeared more than once, in the order
	// their duplicate was encountered. Never contains values.
	DuplicateKeys []string
	// SkippedLines describes malformed lines that were skipped (line
	// number + reason), for surfacing to the user without ever including
	// the line's actual content (which may itself be a secret value).
	SkippedLines []string
}

// parseDotEnv parses .env-format content: KEY=value pairs, one per line,
// blank lines and lines starting with '#' ignored, matching surrounding
// single/double quotes stripped.
//
// This is the single, shared .env parser for the CLI -- both
// `docker dso env import` and `docker dso migrate` call this rather than
// each maintaining their own line-scanning logic. Behavior is preserved
// exactly as it existed in `env import` before this extraction:
//   - "export KEY=value" is NOT specially handled; the literal token before
//     '=' (here "export KEY") becomes the key. This is pre-existing
//     behavior, not a new limitation introduced by migrate.
//   - Multi-line quoted values are NOT supported; bufio.Scanner is
//     line-oriented, so a value starting with an unterminated quote is
//     parsed as-is (quote characters retained) rather than joined with
//     the following line(s).
func parseDotEnv(r io.Reader) (*parseDotEnvResult, error) {
	result := &parseDotEnvResult{Values: make(map[string]string)}

	scanner := bufio.NewScanner(r)
	buf := make([]byte, dotEnvMaxLineSize)
	scanner.Buffer(buf, dotEnvMaxLineSize)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			result.SkippedLines = append(result.SkippedLines, fmt.Sprintf("line %d: no '=' separator found", lineNum))
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}

		if key == "" || strings.Contains(key, "..") {
			result.SkippedLines = append(result.SkippedLines, fmt.Sprintf("line %d: invalid key", lineNum))
			continue
		}
		if len(value) > dotEnvMaxLineSize {
			result.SkippedLines = append(result.SkippedLines, fmt.Sprintf("line %d: value exceeds 1MB", lineNum))
			continue
		}

		if _, exists := result.Values[key]; exists {
			result.DuplicateKeys = append(result.DuplicateKeys, key)
		}
		result.Values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading .env content: %w", err)
	}

	return result, nil
}
