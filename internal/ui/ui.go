// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate npm install
//go:generate npm run build:prod

// Package ui provides embedded assets for the UI.
package ui

import (
	"embed"
	"io/fs"
	"sync"
)

//go:embed templates templates/_*.html public
var files embed.FS

var templateFiles = sub("templates")
var publicFiles = sub("public")

// TemplateFiles returns a filesystem of the embedded template files.
func TemplateFiles() fs.FS {
	return templateFiles()
}

// StaticAssets returns a filesystem of the embedded asset files.
func StaticAssets() fs.FS {
	return publicFiles()
}

func sub(name string) func() fs.FS {
	return sync.OnceValue(func() fs.FS {
		fsys, err := fs.Sub(files, name)
		if err != nil {
			panic(err)
		}
		return fsys
	})
}
