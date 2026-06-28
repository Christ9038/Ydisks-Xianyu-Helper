package webui

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var files embed.FS

// Static returns the embedded built frontend rooted at static/.
func Static() (fs.FS, error) {
	return fs.Sub(files, "static")
}
