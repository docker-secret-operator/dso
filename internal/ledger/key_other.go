//go:build !linux

package ledger

func renameNoReplaceAt(int, string, string) error { return ErrKeyUnsupported }

// LoadKey is unsupported off Linux.
func (p *FileKeyProvider) LoadKey() (HMACKey, error) {
	return HMACKey{}, keyErr(ErrKeyUnsupported, "host-local key management requires Linux", nil)
}

func (p *FileKeyProvider) create(bool) (string, error) {
	return "", keyErr(ErrKeyUnsupported, "host-local key management requires Linux", nil)
}
