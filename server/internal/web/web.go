// Package web embeds the viewer and dashboard so the server is one file.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// FS is the static site rooted at static/.
func FS() fs.FS {
	sub, _ := fs.Sub(files, "static")
	return sub
}
