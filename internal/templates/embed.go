package templates

import (
	"embed"
	"io/fs"
)

//go:embed project
var projectFS embed.FS

func FS() fs.FS {
	return projectFS
}
