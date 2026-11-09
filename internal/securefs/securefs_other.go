//go:build !unix

// Package securefs: on platforms without POSIX ownership and mode semantics
// the central store cannot establish that key material and the database are
// protected, so every check fails closed.
package securefs

import (
	"errors"
	"os"
)

const Supported = false

var errUnsupported = errors.New("securefs: this platform cannot verify file ownership and permissions; " +
	"the central store is supported on unix-like systems only")

func CheckDirChain(string) error                    { return errUnsupported }
func OpenPrivateFile(string) (*os.File, error)      { return nil, errUnsupported }
func CheckOpenFile(*os.File, string) error          { return errUnsupported }
func OpenRegularNoFollow(string) (*os.File, error)  { return nil, errUnsupported }
func ReadPrivateFile(string, int64) ([]byte, error) { return nil, errUnsupported }
func EnsureDir(string) error                        { return errUnsupported }

func CreateExclusive(string, os.FileMode) (*os.File, error) { return nil, errUnsupported }
