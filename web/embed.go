// Package web embeds the built frontend (web/dist) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend files, rooted at the dist directory.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // cannot happen: "dist" is embedded above
	}
	return sub
}
