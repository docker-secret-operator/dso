package webui

import (
	"embed"
	"io/fs"
)

// Assets contains the embedded Next.js static export assets
// Built with: npm run build in the web/ directory
// Assets copied to internal/webui/assets/ during build
//
//go:embed assets/*
var Assets embed.FS

// GetAssets returns the embedded filesystem containing static UI assets
func GetAssets() (fs.FS, error) {
	// Return the assets directory from the embedded filesystem
	return fs.Sub(Assets, "assets")
}
