package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	filepath.WalkDir("internal/cli", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		
		// Skip test files
		if filepath.Base(path) == "root.go" || filepath.Base(path) == "cli_test.go" {
			// We will add var osExit to root.go
		}
		
		replaced := bytes.ReplaceAll(b, []byte("os.Exit"), []byte("osExit"))
		os.WriteFile(path, replaced, 0644)
		return nil
	})
}
