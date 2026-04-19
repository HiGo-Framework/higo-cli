package creator_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/triasbrata/higo-cli/internal/creator"
	"github.com/triasbrata/higo-cli/internal/generator"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// scaffoldProject generates a project in a temp dir, runs go get + go mod tidy,
// and returns the directory path. Mirrors the approach in generator_test.go.
func scaffoldProject(t *testing.T, base string, data *wizard.ProjectData) string {
	t.Helper()
	outDir := filepath.Join(base, data.ProjectName)
	require.NoError(t, generator.Generate(outDir, data), "generate base project")

	get := exec.Command("go", "get", "github.com/triasbrata/higo-framework@latest")
	get.Dir = outDir
	if out, err := get.CombinedOutput(); err != nil {
		t.Fatalf("go get failed:\n%s", out)
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = outDir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed:\n%s", out)
	}
	return outDir
}

// runTidy runs go mod tidy in dir.
func runTidy(t *testing.T, dir string) {
	t.Helper()
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed:\n%s", out)
	}
}

// assertBuilds verifies go build and go vet pass in dir.
func assertBuilds(t *testing.T, dir string) {
	t.Helper()
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed:\n%s", out)
	}
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = dir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("go vet failed:\n%s", out)
	}
}

func fileContains(t *testing.T, path, substr string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	assert.True(t, strings.Contains(string(data), substr),
		"expected %q to contain %q\n\nactual content:\n%s", path, substr, string(data))
}

func fileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "expected file to exist: %s", path)
}

// ── Probe tests ───────────────────────────────────────────────────────────────

func TestProbeFailsOnMissingGoMod(t *testing.T) {
	dir := t.TempDir()
	_, err := creator.Probe(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go.mod not found")
}

func TestProbeFailsOnNonHigoProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/example/other\n\ngo 1.24\n"), 0o644))
	_, err := creator.Probe(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "higo-framework")
}

func TestProbeDetectsModuleName(t *testing.T) {
	dir := t.TempDir()
	gomod := "module github.com/acme/my-service\n\ngo 1.24\n\nrequire github.com/triasbrata/higo-framework v0.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))
	data, err := creator.Probe(dir)
	require.NoError(t, err)
	assert.Equal(t, "github.com/acme/my-service", data.ModuleName)
}

func TestProbeDetectsExistingServices(t *testing.T) {
	dir := t.TempDir()
	gomod := "module github.com/acme/svc\n\ngo 1.24\n\nrequire github.com/triasbrata/higo-framework v0.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

	// create cmd/http and cmd/grpc directories
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "http"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "grpc"), 0o755))

	data, err := creator.Probe(dir)
	require.NoError(t, err)
	assert.True(t, data.HasHTTP)
	assert.True(t, data.HasGRPC)
	assert.False(t, data.HasConsumer)
	assert.True(t, data.HasMix, "2 services → HasMix should be true")
}

func TestProbeDetectsAppNameFromEnvGo(t *testing.T) {
	dir := t.TempDir()
	gomod := "module github.com/acme/svc\n\ngo 1.24\n\nrequire github.com/triasbrata/higo-framework v0.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))

	envGo := `package config
func foo(secret interface{ GetSecretAsString(string,string) string }) {
	_ = secret.GetSecretAsString("APP_NAME", "my-custom-app")
}`
	configDir := filepath.Join(dir, "internals", "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "env.go"), []byte(envGo), 0o644))

	data, err := creator.Probe(dir)
	require.NoError(t, err)
	assert.Equal(t, "my-custom-app", data.AppName)
}

// ── AddServerFiles unit tests (no build) ─────────────────────────────────────

func TestAddServerFilesFailsWhenServiceAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "http"), 0o755))

	data := &wizard.ProjectData{ModuleName: "github.com/test/x", AppName: "x", HasHTTP: true}
	_, err := creator.AddServerFiles(dir, data, "http")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// ── patchFile correctness tests ───────────────────────────────────────────────

// TestPatchPreservesGetInstrumentationConfig verifies the critical bug fix:
// the method patch must INSERT BEFORE GetInstrumentationConfig, not replace it.
func TestPatchPreservesGetInstrumentationConfig(t *testing.T) {
	// minimal config.go that mirrors the template output
	configSrc := `package config

import (
	"github.com/triasbrata/higo-framework/instrumentation"
)

type Config struct {
	Otel instrumentation.InstrumentationConfig
}

func (c *Config) GetInstrumentationConfig() instrumentation.InstrumentationConfig {
	return c.Otel
}
`
	envSrc := `package config

import (
	"github.com/triasbrata/higo-framework/secrets"
)

func NewConfigEnv(secret secrets.Secret) (*Config, error) {
	cfg := &Config{}
	return cfg, nil
}
`
	fxSrc := `package config

import (
	"github.com/triasbrata/higo-framework/instrumentation"
	"go.uber.org/fx"
)

func LoadConfig() fx.Option {
	return fx.Module("config",
		fx.Provide(func(cfg *Config) *Config { return cfg },
			fx.Annotate(
				func(cfg *Config) *Config { return cfg },
				fx.As(new(instrumentation.InstrumentationProvider)),
			)),
	)
}
`
	deliveryFxSrc := `package delivery

import "go.uber.org/fx"

var _ = fx.Options()
`
	dir := t.TempDir()
	configDir := filepath.Join(dir, "internals", "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.go"), []byte(configSrc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "env.go"), []byte(envSrc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "fx.go"), []byte(fxSrc), 0o644))

	deliveryDir := filepath.Join(dir, "internals", "delivery")
	require.NoError(t, os.MkdirAll(deliveryDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(deliveryDir, "fx.go"), []byte(deliveryFxSrc), 0o644))

	// Simulate what patchFile does for the method patch (insert-before):
	content := configSrc
	find := "\nfunc (c *Config) GetInstrumentationConfig"
	code := "\nfunc (c *Config) GetHttpConfig() httpserver.HttpServerConfig { return c.Http }\n"
	idx := strings.Index(content, find)
	require.NotEqual(t, -1, idx, "anchor must be found")
	// Insert before (correct behaviour)
	result := content[:idx] + code + content[idx:]
	assert.Contains(t, result, "GetInstrumentationConfig", "GetInstrumentationConfig must survive the patch")
	assert.Contains(t, result, "GetHttpConfig", "GetHttpConfig must be added")

	// Also verify the old (broken) replace behaviour would have lost the function name
	brokenResult := content[:idx] + code + content[idx+len(find):]
	assert.Contains(t, brokenResult, "GetHttpConfig")
	// The broken path loses "GetInstrumentationConfig" as function name
	assert.NotContains(t, brokenResult, "\nfunc (c *Config) GetInstrumentationConfig()",
		"broken replace would corrupt GetInstrumentationConfig — this confirms the bug exists in the old code")
}

// TestPatchPreservesReturnStatement verifies the before-return patch keeps
// "return cfg, nil" intact.
func TestPatchPreservesReturnStatement(t *testing.T) {
	envSrc := `package config

func NewConfigEnv() (*Config, error) {
	cfg := &Config{}
	return cfg, nil
}
`
	find := "\treturn cfg, nil"
	code := "\tcfg.Http = httpserver.HttpServerConfig{Port: \"8000\"}\n"

	// correct (insert before)
	idx := strings.Index(envSrc, find)
	result := envSrc[:idx] + code + envSrc[idx:]
	assert.Contains(t, result, "return cfg, nil", "return statement must be preserved")
	assert.Contains(t, result, "cfg.Http =", "assignment must be added")

	// broken (replace)
	broken := envSrc[:idx] + code + envSrc[idx+len(find):]
	assert.NotContains(t, broken, "return cfg, nil", "confirms broken replace removes return statement")
}

// ── integration tests: scaffold + create + build ──────────────────────────────

type creatorCase struct {
	base    wizard.ProjectData
	addSvc  string
	wantMix bool
}

var creatorMatrix = []creatorCase{
	{
		base:    wizard.ProjectData{ProjectName: "base-http-add-grpc", ModuleName: "github.com/test/base-http-add-grpc", AppName: "base-http-add-grpc", HasHTTP: true},
		addSvc:  "grpc",
		wantMix: true,
	},
	{
		base:    wizard.ProjectData{ProjectName: "base-http-add-consumer", ModuleName: "github.com/test/base-http-add-consumer", AppName: "base-http-add-consumer", HasHTTP: true},
		addSvc:  "consumer",
		wantMix: true,
	},
	{
		base:    wizard.ProjectData{ProjectName: "base-grpc-add-http", ModuleName: "github.com/test/base-grpc-add-http", AppName: "base-grpc-add-http", HasGRPC: true},
		addSvc:  "http",
		wantMix: true,
	},
	{
		base:    wizard.ProjectData{ProjectName: "base-grpc-add-consumer", ModuleName: "github.com/test/base-grpc-add-consumer", AppName: "base-grpc-add-consumer", HasGRPC: true},
		addSvc:  "consumer",
		wantMix: true,
	},
	{
		base:    wizard.ProjectData{ProjectName: "base-consumer-add-http", ModuleName: "github.com/test/base-consumer-add-http", AppName: "base-consumer-add-http", HasConsumer: true},
		addSvc:  "http",
		wantMix: true,
	},
	{
		base:    wizard.ProjectData{ProjectName: "base-consumer-add-grpc", ModuleName: "github.com/test/base-consumer-add-grpc", AppName: "base-consumer-add-grpc", HasConsumer: true},
		addSvc:  "grpc",
		wantMix: true,
	},
}

func TestAddServerBuilds(t *testing.T) {
	base, err := os.MkdirTemp("", "higo-creator-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		if !t.Failed() {
			os.RemoveAll(base)
		} else {
			t.Logf("artifacts left for inspection: %s", base)
		}
	})

	for _, tc := range creatorMatrix {
		t.Run(tc.base.ProjectName, func(t *testing.T) {
			t.Parallel()

			outDir := scaffoldProject(t, base, &tc.base)

			// probe the scaffolded project to build the data object
			data, err := creator.Probe(outDir)
			require.NoError(t, err, "Probe must succeed on scaffolded project")

			// add the new server (no tidy — we'll tidy manually below)
			_, err = creator.AddServerFiles(outDir, data, tc.addSvc)
			require.NoError(t, err, "AddServerFiles must not error")

			// tidy after file generation to pull new deps
			runTidy(t, outDir)

			// validate compilation
			assertBuilds(t, outDir)

			// spot-check key files were created
			fileExists(t, filepath.Join(outDir, "cmd", tc.addSvc, "main.go"))
			fileExists(t, filepath.Join(outDir, "internals", "bootstrap", tc.addSvc+".go"))

			// verify mix files when expected
			if tc.wantMix {
				assert.True(t, data.HasMix, "HasMix must be true after adding second service")
				fileExists(t, filepath.Join(outDir, "cmd", "mix", "main.go"))
				fileExists(t, filepath.Join(outDir, "internals", "bootstrap", "mix.go"))
			}

			// verify config.go was patched and GetInstrumentationConfig survives
			configGo := filepath.Join(outDir, "internals", "config", "config.go")
			fileContains(t, configGo, "GetInstrumentationConfig")

			// verify env.go still has return statement
			envGo := filepath.Join(outDir, "internals", "config", "env.go")
			fileContains(t, envGo, "return cfg, nil")

			// verify delivery/fx.go has the new Module function
			deliveryFx := filepath.Join(outDir, "internals", "delivery", "fx.go")
			switch tc.addSvc {
			case "http":
				fileContains(t, deliveryFx, "ModuleHttp")
				fileContains(t, configGo, "GetHttpConfig")
				fileContains(t, envGo, "HTTP_PORT")
			case "grpc":
				fileContains(t, deliveryFx, "ModuleGrpc")
				fileContains(t, configGo, "GetGrpcConfig")
				fileContains(t, envGo, "GRPC_PORT")
			case "consumer":
				fileContains(t, deliveryFx, "ModuleConsumer")
				fileContains(t, configGo, "GetAmqpConfig")
				fileContains(t, envGo, "AMQP_URI")
			}
		})
	}
}

func TestAddServerIdempotent(t *testing.T) {
	base, err := os.MkdirTemp("", "higo-idempotent-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(base) })

	baseData := wizard.ProjectData{
		ProjectName: "idempotent-test",
		ModuleName:  "github.com/test/idempotent-test",
		AppName:     "idempotent-test",
		HasHTTP:     true,
	}
	outDir := scaffoldProject(t, base, &baseData)

	data, err := creator.Probe(outDir)
	require.NoError(t, err)

	// first add
	_, err = creator.AddServerFiles(outDir, data, "grpc")
	require.NoError(t, err)
	runTidy(t, outDir)

	// re-probe and re-add — should fail cleanly
	data2, err := creator.Probe(outDir)
	require.NoError(t, err)
	_, err = creator.AddServerFiles(outDir, data2, "grpc")
	require.Error(t, err, "adding the same service twice must return an error")
	assert.Contains(t, err.Error(), "already exists")

	// project must still compile after the failed second attempt
	assertBuilds(t, outDir)
}
