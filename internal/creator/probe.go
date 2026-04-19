package creator

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/triasbrata/higo-cli/internal/wizard"
)

const higoFrameworkMod = "github.com/triasbrata/higo-framework"

// Probe reads the current directory and returns a ProjectData describing
// the existing project state. Returns an error if the directory is not a
// valid higo-framework project.
func Probe(root string) (*wizard.ProjectData, error) {
	modPath := filepath.Join(root, "go.mod")
	modData, err := os.ReadFile(modPath)
	if err != nil {
		return nil, fmt.Errorf("go.mod not found — run this command from the project root")
	}

	moduleName := parseModuleName(string(modData))
	if moduleName == "" {
		return nil, fmt.Errorf("could not parse module name from go.mod")
	}

	if !strings.Contains(string(modData), higoFrameworkMod) {
		return nil, fmt.Errorf("this project does not use %s", higoFrameworkMod)
	}

	hasHTTP := dirExists(filepath.Join(root, "cmd", "http"))
	hasGRPC := dirExists(filepath.Join(root, "cmd", "grpc"))
	hasConsumer := dirExists(filepath.Join(root, "cmd", "consumer"))

	serviceCount := 0
	for _, v := range []bool{hasHTTP, hasGRPC, hasConsumer} {
		if v {
			serviceCount++
		}
	}

	return &wizard.ProjectData{
		ModuleName:   moduleName,
		AppName:      detectAppName(root, moduleName),
		HasHTTP:      hasHTTP,
		HasGRPC:      hasGRPC,
		HasConsumer:  hasConsumer,
		HasMix:       serviceCount > 1,
		HasPyroscope: detectPyroscope(root),
	}, nil
}

func parseModuleName(gomod string) string {
	scanner := bufio.NewScanner(strings.NewReader(gomod))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

var appNameRe = regexp.MustCompile(`GetSecretAsString\s*\(\s*"APP_NAME"\s*,\s*"([^"]+)"`)

func detectAppName(root, moduleName string) string {
	envPath := filepath.Join(root, "internals", "config", "env.go")
	if data, err := os.ReadFile(envPath); err == nil {
		if m := appNameRe.FindSubmatch(data); m != nil {
			return string(m[1])
		}
	}
	// fallback: last segment of module path
	parts := strings.Split(moduleName, "/")
	return parts[len(parts)-1]
}

func detectPyroscope(root string) bool {
	bootstrapDir := filepath.Join(root, "internals", "bootstrap")
	entries, err := os.ReadDir(bootstrapDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(bootstrapDir, e.Name()))
		if err != nil {
			continue
		}
		// pyroscope is enabled when LoadPyroscope() is called (not the disabled variant)
		if strings.Contains(string(data), "LoadPyroscope()") &&
			!strings.Contains(string(data), "LoadDisabledProfiler()") {
			return true
		}
	}
	return false
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
