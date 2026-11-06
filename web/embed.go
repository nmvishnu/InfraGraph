package web

import (
	"embed"
	"net/http"
)

//go:embed index.html app.js style.css
var assets embed.FS

// AssetFS returns an http.FileSystem serving the embedded web assets.
func AssetFS() http.FileSystem {
	return http.FS(assets)
}
