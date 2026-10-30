package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// GetFileSystem returns the embedded static files as an http.FileSystem
func GetFileSystem() http.FileSystem {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

