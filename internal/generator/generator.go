package generator

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/triasbrata/higo-cli/internal/templates"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

func Generate(outputDir string, data *wizard.ProjectData) error {
	tmplFS := templates.FS()

	return fs.WalkDir(tmplFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath := strings.TrimPrefix(path, "project/")
		if relPath == "" {
			return nil
		}

		if !shouldInclude(relPath, data) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		outPath := filepath.Join(outputDir, filepath.FromSlash(relPath))
		outPath = strings.TrimSuffix(outPath, ".tmpl")
		// rename gitignore → .gitignore in output
		base := filepath.Base(outPath)
		if base == "gitignore" {
			outPath = filepath.Join(filepath.Dir(outPath), ".gitignore")
		}
		if base == "env.example" {
			outPath = filepath.Join(filepath.Dir(outPath), ".env.example")
		}

		if d.IsDir() {
			return os.MkdirAll(outPath, 0o755)
		}

		content, err := fs.ReadFile(tmplFS, path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", path, err)
		}

		rendered, err := render(path, string(content), data)
		if err != nil {
			return fmt.Errorf("render %s: %w", path, err)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}

		return os.WriteFile(outPath, []byte(rendered), 0o644)
	})
}

func render(name, content string, data *wizard.ProjectData) (string, error) {
	tmpl, err := template.New(name).Delims("[[", "]]").Parse(content)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func shouldInclude(relPath string, data *wizard.ProjectData) bool {
	if hasPathPrefix(relPath, "cmd/http") || hasPathPrefix(relPath, "internals/bootstrap/http") ||
		hasPathPrefix(relPath, "internals/delivery/http") {
		return data.HasHTTP
	}
	if hasPathPrefix(relPath, "cmd/grpc") || hasPathPrefix(relPath, "internals/bootstrap/grpc") ||
		hasPathPrefix(relPath, "internals/delivery/grpc") {
		return data.HasGRPC
	}
	if hasPathPrefix(relPath, "cmd/consumer") || hasPathPrefix(relPath, "internals/bootstrap/consumer") ||
		hasPathPrefix(relPath, "internals/delivery/consumer") {
		return data.HasConsumer
	}
	if hasPathPrefix(relPath, "cmd/mix") {
		return data.HasMix
	}
	if hasPathPrefix(relPath, "internals/bootstrap/mix") {
		return data.HasMix
	}
	return true
}

func hasPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/") || strings.HasPrefix(path, prefix+".")
}
