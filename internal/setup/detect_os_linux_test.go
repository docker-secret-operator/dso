//go:build linux

package setup

import (
	"errors"
	"testing"
)

func TestParseOSRelease_ValidContent(t *testing.T) {
	data := []byte(`NAME="Ubuntu"\nID=ubuntu\nVERSION_ID="22.04"\n`)
	distro, version := parseOSRelease(data)
	if distro != "ubuntu" {
		t.Errorf("distro: want 'ubuntu', got %q", distro)
	}
	if version != "22.04" {
		t.Errorf("version: want '22.04', got %q", version)
	}
}

func TestParseOSRelease_UnquotedValues(t *testing.T) {
	data := []byte("ID=debian\nVERSION_ID=12\n")
	distro, version := parseOSRelease(data)
	if distro != "debian" {
		t.Errorf("distro: want 'debian', got %q", distro)
	}
	if version != "12" {
		t.Errorf("version: want '12', got %q", version)
	}
}

func TestParseOSRelease_CorruptedContent_EmptyResult(t *testing.T) {
	data := []byte("this is not valid os-release content !!!")
	distro, version := parseOSRelease(data)
	if distro != "" || version != "" {
		t.Errorf("expected empty results for corrupted content, got distro=%q version=%q", distro, version)
	}
}

func TestParseOSRelease_EmptyContent(t *testing.T) {
	distro, version := parseOSRelease([]byte{})
	if distro != "" || version != "" {
		t.Errorf("expected empty results for empty content, got distro=%q version=%q", distro, version)
	}
}

func TestParsePlatformOSInfo_ReadError_EmitsWarning(t *testing.T) {
	_, _, warn := parsePlatformOSInfo(func(_ string) ([]byte, error) {
		return nil, errors.New("permission denied")
	})
	if warn == nil {
		t.Fatal("expected non-nil warning when ReadFile fails")
	}
	if warn.Code != "os_release_read_failed" {
		t.Errorf("warn.Code: want os_release_read_failed, got %q", warn.Code)
	}
}

func TestParsePlatformOSInfo_ValidContent_NoWarning(t *testing.T) {
	data := []byte("ID=alpine\nVERSION_ID=3.18\n")
	distro, version, warn := parsePlatformOSInfo(func(_ string) ([]byte, error) {
		return data, nil
	})
	if warn != nil {
		t.Errorf("expected no warning, got: %v", warn)
	}
	if distro != "alpine" {
		t.Errorf("distro: want 'alpine', got %q", distro)
	}
	if version != "3.18" {
		t.Errorf("version: want '3.18', got %q", version)
	}
}
