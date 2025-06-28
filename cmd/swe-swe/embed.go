package main

import (
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
)

//go:embed index.html.tmpl
var indexTemplate string

//go:embed static/css/*.css static/js/*.js
var staticFiles embed.FS

// AssetInfo holds information about an embedded asset
type AssetInfo struct {
	Path string
	Hash string
}

// getAssetHash calculates SHA1 hash of an asset file
func getAssetHash(data []byte) string {
	hash := sha1.Sum(data)
	return hex.EncodeToString(hash[:])[:8] // Use first 8 chars of hash
}

// getStaticAssets returns a map of asset paths to their hashed versions
func getStaticAssets() (map[string]AssetInfo, error) {
	assets := make(map[string]AssetInfo)

	// Walk through embedded files
	err := fs.WalkDir(staticFiles, "static", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Read file content
		content, err := staticFiles.ReadFile(filePath)
		if err != nil {
			return err
		}

		// Calculate hash
		hash := getAssetHash(content)

		// Extract relative path from "static/"
		relativePath := strings.TrimPrefix(filePath, "static/")

		// Create hashed filename
		dir := path.Dir(relativePath)
		base := path.Base(relativePath)
		ext := path.Ext(base)
		nameWithoutExt := strings.TrimSuffix(base, ext)

		hashedPath := path.Join(dir, nameWithoutExt+"."+hash+ext)

		assets[relativePath] = AssetInfo{
			Path: hashedPath,
			Hash: hash,
		}

		return nil
	})

	return assets, err
}

// getFileContent returns the content of an embedded static file
func getFileContent(originalPath string) ([]byte, error) {
	return staticFiles.ReadFile("static/" + originalPath)
}
