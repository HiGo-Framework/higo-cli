package creator

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"github.com/triasbrata/higo-cli/internal/templates"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

// AddServer adds a new server type and runs go mod tidy afterward.
func AddServer(root string, data *wizard.ProjectData, svc string) error {
	steps, err := AddServerFiles(root, data, svc)
	if err != nil {
		return err
	}

	fmt.Printf("\n  ✓  %s server added successfully\n\n", svc)
	for _, s := range steps {
		fmt.Printf("     %s\n", s)
	}
	fmt.Println()

	return tidyProject(root)
}

// tidyProject runs go mod download then go mod tidy.
// download populates go.sum with hashes for newly-imported sub-packages;
// tidy then removes any unused entries.
func tidyProject(root string) error {
	run := func(label string, args ...string) error {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("  → " + label))
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	if err := run("go mod download…", "mod", "download"); err != nil {
		return fmt.Errorf("go mod download: %w", err)
	}
	if err := run("go mod tidy…", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("  ✓ dependencies updated"))
	return nil
}

// AddServerFiles performs all file writes/patches without running go mod tidy.
// Exported so tests can call it without the bubbletea spinner.
func AddServerFiles(root string, data *wizard.ProjectData, svc string) ([]string, error) {
	if svcExists(root, svc) {
		return nil, fmt.Errorf("%s server already exists in this project", svc)
	}

	switch svc {
	case "http":
		data.HasHTTP = true
	case "grpc":
		data.HasGRPC = true
	case "consumer":
		data.HasConsumer = true
	}

	count := 0
	for _, v := range []bool{data.HasHTTP, data.HasGRPC, data.HasConsumer} {
		if v {
			count++
		}
	}
	data.HasMix = count > 1

	var steps []string

	// cmd/{svc}/main.go — tiny bootstrap call, no user code
	if err := writeFromTemplate(root, data, "cmd/"+svc+"/main.go.tmpl", "cmd/"+svc+"/main.go"); err != nil {
		return nil, err
	}
	steps = append(steps, "cmd/"+svc+"/main.go")

	// internals/bootstrap/{svc}.go — framework wiring
	if err := writeFromTemplate(root, data, "internals/bootstrap/"+svc+".go.tmpl", "internals/bootstrap/"+svc+".go"); err != nil {
		return nil, err
	}
	steps = append(steps, "internals/bootstrap/"+svc+".go")

	// delivery scaffold — skip files that already exist (user may have edited them)
	scaffoldFiles, err := writeDeliveryScaffold(root, data, svc)
	if err != nil {
		return nil, err
	}
	steps = append(steps, scaffoldFiles...)

	// regenerate shared config + delivery/fx files from templates.
	// Templates already handle every combination via [[- if .HasX]] guards,
	// so this is idempotent across service combos (unlike text-patching,
	// which could corrupt anchors after 2+ patches).
	if err := regenerateSharedFiles(root, data); err != nil {
		return nil, err
	}
	steps = append(steps, "internals/config/{config,env,fx}.go (regenerated)")
	steps = append(steps, "internals/delivery/fx.go (regenerated)")
	steps = append(steps, ".env.example, Taskfile.yml (regenerated)")

	// regenerate mix when 2+ services are active
	if data.HasMix {
		if err := writeFromTemplate(root, data, "cmd/mix/main.go.tmpl", "cmd/mix/main.go"); err != nil {
			return nil, err
		}
		if err := writeFromTemplate(root, data, "internals/bootstrap/mix.go.tmpl", "internals/bootstrap/mix.go"); err != nil {
			return nil, err
		}
		steps = append(steps, "cmd/mix/main.go", "internals/bootstrap/mix.go")
	}

	return steps, nil
}

func svcExists(root, svc string) bool {
	return dirExists(filepath.Join(root, "cmd", svc))
}

func writeFromTemplate(root string, data *wizard.ProjectData, tmplPath, outRelPath string) error {
	tmplFS := templates.FS()
	content, err := fs.ReadFile(tmplFS, "project/"+tmplPath)
	if err != nil {
		return fmt.Errorf("template not found: %s", tmplPath)
	}
	tmpl, err := template.New(tmplPath).Delims("[[", "]]").Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", tmplPath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render template %s: %w", tmplPath, err)
	}
	outPath := filepath.Join(root, filepath.FromSlash(outRelPath))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, buf.Bytes(), 0o644)
}

func writeDeliveryScaffold(root string, data *wizard.ProjectData, svc string) ([]string, error) {
	type fileSpec struct {
		rel  string
		tmpl string
	}
	var files []fileSpec
	switch svc {
	case "http":
		files = []fileSpec{
			{"internals/delivery/http/handler.go", httpHandlerTmpl},
			{"internals/delivery/http/router.go", httpRouterTmpl},
			{"internals/delivery/http/impl/init.go", httpImplInitTmpl},
		}
	case "grpc":
		files = []fileSpec{
			{"internals/delivery/grpc/handler.go", grpcHandlerTmpl},
			{"internals/delivery/grpc/impl/init.go", grpcImplInitTmpl},
		}
	case "consumer":
		files = []fileSpec{
			{"internals/delivery/consumer/consumer.go", consumerHandlerTmpl},
			{"internals/delivery/consumer/routing.go", consumerRoutingTmpl},
			{"internals/delivery/consumer/impl/init.go", consumerImplInitTmpl},
		}
	}

	var written []string
	for _, f := range files {
		outPath := filepath.Join(root, filepath.FromSlash(f.rel))
		if _, err := os.Stat(outPath); err == nil {
			continue // skip existing user files
		}
		rendered, err := renderInline(f.tmpl, data)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", f.rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(outPath, []byte(rendered), 0o644); err != nil {
			return nil, err
		}
		written = append(written, f.rel)
	}
	return written, nil
}

func renderInline(tmplStr string, data *wizard.ProjectData) (string, error) {
	tmpl, err := template.New("inline").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ── shared file regeneration ──────────────────────────────────────────────────

// Repair re-renders the shared config + delivery wiring files from templates,
// based on the current on-disk project state (via Probe). Useful when those
// files get corrupted (e.g. legacy text-patching or manual edits gone wrong).
func Repair(root string) ([]string, error) {
	data, err := Probe(root)
	if err != nil {
		return nil, err
	}
	if err := regenerateSharedFiles(root, data); err != nil {
		return nil, err
	}
	return []string{
		"internals/config/config.go",
		"internals/config/env.go",
		"internals/config/fx.go",
		"internals/delivery/fx.go",
		".env.example",
		"Taskfile.yml",
	}, nil
}

// regenerateSharedFiles re-renders config.go, env.go, fx.go, and delivery/fx.go
// from their templates. Safe to call repeatedly: the templates use [[- if .HasX]]
// guards so the output always reflects the full set of enabled services in data.
func regenerateSharedFiles(root string, data *wizard.ProjectData) error {
	shared := []struct {
		tmpl, out string
	}{
		{"internals/config/config.go.tmpl", "internals/config/config.go"},
		{"internals/config/env.go.tmpl", "internals/config/env.go"},
		{"internals/config/fx.go.tmpl", "internals/config/fx.go"},
		{"internals/delivery/fx.go.tmpl", "internals/delivery/fx.go"},
		{"env.example.tmpl", ".env.example"},
		{"Taskfile.yml.tmpl", "Taskfile.yml"},
	}
	for _, f := range shared {
		if err := writeFromTemplate(root, data, f.tmpl, f.out); err != nil {
			return fmt.Errorf("regenerate %s: %w", f.out, err)
		}
	}
	return nil
}


// ── delivery scaffold inline templates ────────────────────────────────────────

const httpHandlerTmpl = `package http

// Handler defines the HTTP handler interface.
// Add your handler methods here and implement them in impl/.
// import "github.com/gofiber/fiber/v2" when you add methods.
type Handler interface {
	// Example: Hello(c *fiber.Ctx) error
}
`

const httpRouterTmpl = `package http

import (
	"github.com/triasbrata/higo-framework/routers"
	fhttp "github.com/triasbrata/higo-framework/server/http"
	"go.uber.org/fx"
)

// Param groups the router's fx dependencies.
type Param struct {
	fx.In
	Router  routers.Router
	Handler Handler
}

// NewRouter registers HTTP routes.
func NewRouter(p Param) fhttp.RoutingBind {
	return func() error {
		// p.Router.Get("/example", p.Handler.YourMethod)
		return nil
	}
}
`

const httpImplInitTmpl = `package impl

import (
	"log/slog"

	deliveryHttp "{{.ModuleName}}/internals/delivery/http"
)

type httpHandler struct {
	log *slog.Logger
}

// NewHandler constructs the HTTP handler implementation.
func NewHandler(log *slog.Logger) deliveryHttp.Handler {
	return &httpHandler{log: log}
}
`

const grpcHandlerTmpl = `package grpc

import "context"

// Handler is the gRPC service handler interface.
// Replace with your proto-generated service interface after running buf generate.
type Handler interface {
	Ping(ctx context.Context) error
}
`

const grpcImplInitTmpl = `package impl

import (
	"context"

	deliveryGrpc "{{.ModuleName}}/internals/delivery/grpc"
)

type grpcHandler struct{}

// NewGrpcHandler constructs the gRPC handler implementation.
func NewGrpcHandler() deliveryGrpc.Handler {
	return &grpcHandler{}
}

func (h *grpcHandler) Ping(_ context.Context) error {
	return nil
}
`

const consumerHandlerTmpl = `package consumer

import cmr "github.com/triasbrata/higo-framework/messagebroker/consumer"

// ConsumerHandler defines the message handler interface.
type ConsumerHandler interface {
	HandleMessage(c cmr.CtxConsumer) error
}
`

const consumerRoutingTmpl = `package consumer

import (
	cmr "github.com/triasbrata/higo-framework/messagebroker/consumer"
	"github.com/triasbrata/higo-framework/middleware"
)

// NewRoutingConsumer wires queue bindings for the consumer.
func NewRoutingConsumer(handler ConsumerHandler, builder cmr.ConsumerBuilder) {
	builder.Use(middleware.OtelConsumerExtract())
	builder.Consume("{{.AppName}}.messages",
		cmr.WithAmqpTopology(cmr.AmqpTopologyConsumer{PrefetchCount: 10}),
		handler.HandleMessage,
	)
}
`

const consumerImplInitTmpl = `package impl

import (
	"fmt"

	"{{.ModuleName}}/internals/delivery/consumer"
	"github.com/triasbrata/higo-framework/instrumentation"
	cmr "github.com/triasbrata/higo-framework/messagebroker/consumer"
)

type handler struct{}

func (h *handler) HandleMessage(c cmr.CtxConsumer) error {
	ctx, span := instrumentation.Tracer().Start(c.UserContext(), "delivery:consumer:HandleMessage")
	defer span.End()
	_ = ctx
	fmt.Printf("received message: %s\n", c.Body())
	return nil
}

// NewHandlerConsumer constructs the consumer message handler.
func NewHandlerConsumer() consumer.ConsumerHandler {
	return &handler{}
}
`
