package crypto

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestFieldLen_Boundaries(t *testing.T) {
	ok := []int{0, 1, MaxAADFieldSize}
	for _, n := range ok {
		got, err := fieldLen(n)
		if err != nil || int(got) != n {
			t.Errorf("fieldLen(%d) = %d, %v; want %d, nil", n, got, err, n)
		}
	}
	bad := []int{-1, math.MinInt, MaxAADFieldSize + 1, math.MaxUint32, math.MaxUint32 + 1, math.MaxInt}
	for _, n := range bad {
		if _, err := fieldLen(n); !errors.Is(err, ErrAADFieldTooLarge) {
			t.Errorf("fieldLen(%d) must be rejected, got %v", n, err)
		}
	}
}

func TestAAD_RejectsOversizedFieldBeforeEncoding(t *testing.T) {
	huge := strings.Repeat("a", MaxAADFieldSize+1)
	if b, err := aad("d", huge); !errors.Is(err, ErrAADFieldTooLarge) || b != nil {
		t.Fatalf("want ErrAADFieldTooLarge and no output, got %d bytes, %v", len(b), err)
	}
	if _, err := SecretAAD("p", "e", huge, "n", 1, 1); !errors.Is(err, ErrAADFieldTooLarge) {
		t.Errorf("SecretAAD: %v", err)
	}
	if _, err := DEKAAD(huge, "k", 1); !errors.Is(err, ErrAADFieldTooLarge) {
		t.Errorf("DEKAAD: %v", err)
	}
	if _, err := BindingAAD(huge); !errors.Is(err, ErrAADFieldTooLarge) {
		t.Errorf("BindingAAD: %v", err)
	}
	if _, err := aad(huge); !errors.Is(err, ErrAADFieldTooLarge) {
		t.Errorf("oversized domain: %v", err)
	}
	// The largest accepted field still encodes with the exact length prefix.
	maxOK := strings.Repeat("a", MaxAADFieldSize)
	b, err := aad("d", maxOK)
	if err != nil {
		t.Fatal(err)
	}
	if want := 4 + 1 + 4 + MaxAADFieldSize; len(b) != want {
		t.Errorf("len = %d, want %d", len(b), want)
	}
}

// Valid existing ciphertext must keep opening: the encoding of normal inputs is
// byte-for-byte what it was (4-byte big-endian length, then the bytes).
func TestAAD_EncodingIsUnchangedForValidInput(t *testing.T) {
	got, err := aad("dom", "ab", "")
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0, 0, 0, 3}, "dom"...)
	want = append(want, 0, 0, 0, 2, 'a', 'b', 0, 0, 0, 0)
	if !bytes.Equal(got, want) {
		t.Errorf("encoding changed:\n got %v\nwant %v", got, want)
	}
}

func TestOpen_MalformedInputsFailClosed(t *testing.T) {
	key, _ := GenerateKey()
	a, _ := aad("d")
	nonce, ct, err := Seal(key, []byte("x"), a)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ n, c []byte }{
		"nil nonce":           {nil, ct},
		"short nonce":         {nonce[:NonceSize-1], ct},
		"long nonce":          {append(append([]byte{}, nonce...), 0), ct},
		"empty ciphertext":    {nonce, nil},
		"truncated tag":       {nonce, ct[:len(ct)-1]},
		"ciphertext too tiny": {nonce, []byte{1}},
	}
	for name, c := range cases {
		if _, err := Open(key, c.n, c.c, a); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: want ErrDecrypt, got %v", name, err)
		}
	}
	if _, _, err := Seal(key, make([]byte, MaxPlaintextSize+1), a); !errors.Is(err, ErrPlaintextTooLarge) {
		t.Errorf("oversized plaintext: %v", err)
	}
	if _, _, err := Seal(key, make([]byte, MaxPlaintextSize), a); err != nil {
		t.Errorf("max plaintext must be accepted: %v", err)
	}
}
