package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

func Assets() (fs.FS, error) { return fs.Sub(embedded, "dist") }
