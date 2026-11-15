//go:build linux

package ledger

import (
	"crypto/sha256"
	"encoding/json"
)

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
