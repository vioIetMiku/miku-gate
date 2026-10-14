package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var artwork embed.FS
var certificateRegistry *DomainRegistry

func brandAssets() http.Handler {
	root, _ := fs.Sub(artwork, "assets")
	files := http.StripPrefix("/assets/", http.FileServer(http.FS(root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
