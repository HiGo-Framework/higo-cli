package creator

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

// AddServer adds a new server type to an existing higo project.
// data must come from Probe(). svc is one of "http", "grpc", "consumer".
func AddServer(root string, data *wizard.ProjectData, svc string) error {
	// guard: already exists
	if svcExists(root, svc) {
		return fmt.Errorf("%s server already exists in this project", svc)
	}

	// update flags for the new state
	switch svc {
	case "http":
		data.HasHTTP = true
	case "grpc":
		data.HasGRPC = true
	case "consumer":
		data.HasConsumer = true
	}

	// recompute mix
	count := 0
	for _, v := range []bool{data.HasHTTP, data.HasGRPC, data.HasConsumer} {
		if v {
			count++
		}
	}
	data.HasMix = count > 1

	var steps []string

	// 1. cmd/{svc}/main.go  — always write (4-line bootstrap call, no user code)
	if err := writeFromTemplate(root, data, "cmd/"+svc+"/main.go.tmpl", "cmd/"+svc+"/main.go"); err != nil {
		return err
	}
	steps = append(steps, "cmd/"+svc+"/main.go")

	// 2. internals/bootstrap/{svc}.go — always write (framework wiring)
	if err := writeFromTemplate(root, data, "internals/bootstrap/"+svc+".go.tmpl", "internals/bootstrap/"+svc+".go"); err != nil {
		return err
	}
	steps = append(steps, "internals/bootstrap/"+svc+".go")

	// 3. delivery scaffold files — skip if they already exist (user-editable)
	scaffoldFiles, err := writeDeliveryScaffold(root, data, svc)
	if err != nil {
		return err
	}
	steps = append(steps, scaffoldFiles...)

	// 4. patch shared config files
	if err := patchConfigFiles(root, svc, data); err != nil {
		return err
	}
	steps = append(steps, "internals/config/{config,env,fx}.go (patched)")

	// 5. patch delivery/fx.go — append Module function + imports
	if err := patchDeliveryFx(root, data, svc); err != nil {
		return err
	}
	steps = append(steps, "internals/delivery/fx.go (patched)")

	// 6. mix — always regenerate when there are 2+ services
	if data.HasMix {
		if err := writeFromTemplate(root, data, "cmd/mix/main.go.tmpl", "cmd/mix/main.go"); err != nil {
			return err
		}
		if err := writeFromTemplate(root, data, "internals/bootstrap/mix.go.tmpl", "internals/bootstrap/mix.go"); err != nil {
			return err
		}
		steps = append(steps, "cmd/mix/main.go", "internals/bootstrap/mix.go")
	}

	// summary
	fmt.Printf("\n  ✓  %s server added successfully\n\n", svc)
	for _, s := range steps {
		fmt.Printf("     %s\n", s)
	}
	fmt.Println()
	return nil
}

func svcExists(root, svc string) bool {
	return dirExists(filepath.Join(root, "cmd", svc))
}

// writeFromTemplate renders a single template file from the embedded FS and writes it to disk.
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

// writeDeliveryScaffold creates the delivery layer files for the new service.
// Files that already exist are skipped (user may have customised them).
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

// ── config patching ───────────────────────────────────────────────────────────

func patchConfigFiles(root, svc string, data *wizard.ProjectData) error {
	p := configPatch(svc, data)
	if p == nil {
		return nil
	}

	configGo := filepath.Join(root, "internals", "config", "config.go")
	envGo := filepath.Join(root, "internals", "config", "env.go")
	fxGo := filepath.Join(root, "internals", "config", "fx.go")

	if err := patchFile(configGo, p.configPatches); err != nil {
		return fmt.Errorf("patch config.go: %w", err)
	}
	if err := patchFile(envGo, p.envPatches); err != nil {
		return fmt.Errorf("patch env.go: %w", err)
	}
	if err := patchFile(fxGo, p.fxPatches); err != nil {
		return fmt.Errorf("patch config/fx.go: %w", err)
	}
	return nil
}

type patch struct {
	// sentinel string that must NOT be present for the patch to apply
	sentinel string
	// find is the string to search for as insertion anchor
	find string
	// insertAfter: if true, insert after find; if false, insert before find
	insertAfter bool
	// code to insert
	code string
}

type servicePatch struct {
	configPatches []patch
	envPatches    []patch
	fxPatches     []patch
}

func configPatch(svc string, data *wizard.ProjectData) *servicePatch {
	switch svc {
	case "http":
		return &servicePatch{
			configPatches: []patch{
				{
					sentinel:    `httpserver "github.com/triasbrata/higo-framework/server/http"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\thttpserver \"github.com/triasbrata/higo-framework/server/http\"\n",
				},
				{
					sentinel:    "Http httpserver.HttpServerConfig",
					find:        "type Config struct {",
					insertAfter: false,
					code:        "\ntype Config struct {\n\tHttp httpserver.HttpServerConfig",
				},
				{
					sentinel: "GetHttpConfig",
					find:     "\nfunc (c *Config) GetInstrumentationConfig",
					code:     "\nfunc (c *Config) GetHttpConfig() httpserver.HttpServerConfig { return c.Http }\n",
				},
			},
			envPatches: []patch{
				{
					sentinel:    `httpserver "github.com/triasbrata/higo-framework/server/http"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\thttpserver \"github.com/triasbrata/higo-framework/server/http\"\n",
				},
				{
					sentinel: "HTTP_PORT",
					find:     "\treturn cfg, nil",
					code: "\tcfg.Http = httpserver.HttpServerConfig{\n" +
						"\t\tPort:    secret.GetSecretAsString(\"HTTP_PORT\", \"8000\"),\n" +
						"\t\tAddress: secret.GetSecretAsString(\"HTTP_ADDRESS\", \"\"),\n" +
						"\t}\n",
				},
			},
			fxPatches: []patch{
				{
					sentinel:    `httpserver "github.com/triasbrata/higo-framework/server/http"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\thttpserver \"github.com/triasbrata/higo-framework/server/http\"\n",
				},
				{
					sentinel: "httpserver.HttpConfigProvider",
					find:     "fx.As(new(instrumentation.InstrumentationProvider)),",
					insertAfter: true,
					code:     "\n\t\t\tfx.As(new(httpserver.HttpConfigProvider)),",
				},
			},
		}

	case "grpc":
		return &servicePatch{
			configPatches: []patch{
				{
					sentinel:    `grpcserver "github.com/triasbrata/higo-framework/server/grpc"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\tgrpcserver \"github.com/triasbrata/higo-framework/server/grpc\"\n",
				},
				{
					sentinel: "Grpc grpcserver.GrpcServerConfig",
					find:     "type Config struct {",
					insertAfter: false,
					code:     "\ntype Config struct {\n\tGrpc grpcserver.GrpcServerConfig",
				},
				{
					sentinel: "GetGrpcConfig",
					find:     "\nfunc (c *Config) GetInstrumentationConfig",
					code:     "\nfunc (c *Config) GetGrpcConfig() grpcserver.GrpcServerConfig { return c.Grpc }\n",
				},
			},
			envPatches: []patch{
				{
					sentinel:    `grpcserver "github.com/triasbrata/higo-framework/server/grpc"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\tgrpcserver \"github.com/triasbrata/higo-framework/server/grpc\"\n",
				},
				{
					sentinel: "GRPC_PORT",
					find:     "\treturn cfg, nil",
					code: "\tcfg.Grpc = grpcserver.GrpcServerConfig{\n" +
						"\t\tEnableReflection: true,\n" +
						"\t\tPort:             secret.GetSecretAsString(\"GRPC_PORT\", \"8001\"),\n" +
						"\t\tAddress:          secret.GetSecretAsString(\"GRPC_ADDRESS\", \"\"),\n" +
						"\t}\n",
				},
			},
			fxPatches: []patch{
				{
					sentinel:    `grpcserver "github.com/triasbrata/higo-framework/server/grpc"`,
					find:        "import (\n",
					insertAfter: true,
					code:        "\tgrpcserver \"github.com/triasbrata/higo-framework/server/grpc\"\n",
				},
				{
					sentinel: "grpcserver.GrpcConfigProvider",
					find:     "fx.As(new(instrumentation.InstrumentationProvider)),",
					insertAfter: true,
					code:     "\n\t\t\tfx.As(new(grpcserver.GrpcConfigProvider)),",
				},
			},
		}

	case "consumer":
		return &servicePatch{
			configPatches: []patch{
				{
					sentinel:    `"github.com/triasbrata/higo-framework/messagebroker/broker/impl"`,
					find:        "import (\n",
					insertAfter: true,
					code: "\timpl \"github.com/triasbrata/higo-framework/messagebroker/broker/impl\"\n" +
						"\tserverConsumer \"github.com/triasbrata/higo-framework/server/consumer\"\n",
				},
				{
					sentinel: "Amqp     impl.AmqpConfig",
					find:     "type Config struct {",
					insertAfter: false,
					code: "\ntype Config struct {\n" +
						"\tAmqp     impl.AmqpConfig\n" +
						"\tConsumer serverConsumer.ConsumerServerConfig",
				},
				{
					sentinel: "GetAmqpConfig",
					find:     "\nfunc (c *Config) GetInstrumentationConfig",
					code: "\nfunc (c *Config) GetAmqpConfig() impl.AmqpConfig { return c.Amqp }\n" +
						"func (c *Config) GetConsumerConfig() serverConsumer.ConsumerServerConfig { return c.Consumer }\n",
				},
			},
			envPatches: []patch{
				{
					sentinel:    `"github.com/triasbrata/higo-framework/messagebroker/broker/impl"`,
					find:        "import (\n",
					insertAfter: true,
					code: "\timpl \"github.com/triasbrata/higo-framework/messagebroker/broker/impl\"\n" +
						"\tserverConsumer \"github.com/triasbrata/higo-framework/server/consumer\"\n" +
						"\t\"time\"\n",
				},
				{
					sentinel: "AMQP_URI",
					find:     "\treturn cfg, nil",
					code: "\tcfg.Amqp = impl.AmqpConfig{\n" +
						"\t\tConnectionName: secret.GetSecretAsString(\"AMQP_CONNECTION_NAME\", \"" + data.AppName + "-consumer\"),\n" +
						"\t\tURI:            secret.GetSecretAsString(\"AMQP_URI\", \"amqp://guest:guest@localhost:5672\"),\n" +
						"\t}\n" +
						"\tcfg.Consumer = serverConsumer.ConsumerServerConfig{\n" +
						"\t\tRestartTime: 5 * time.Second,\n" +
						"\t}\n",
				},
			},
			fxPatches: []patch{
				{
					sentinel:    `"github.com/triasbrata/higo-framework/messagebroker"`,
					find:        "import (\n",
					insertAfter: true,
					code: "\t\"github.com/triasbrata/higo-framework/messagebroker\"\n" +
						"\tserverConsumer \"github.com/triasbrata/higo-framework/server/consumer\"\n",
				},
				{
					sentinel: "messagebroker.AmqpConfigProvider",
					find:     "fx.As(new(instrumentation.InstrumentationProvider)),",
					insertAfter: true,
					code: "\n\t\t\tfx.As(new(messagebroker.AmqpConfigProvider))," +
						"\n\t\t\tfx.As(new(serverConsumer.ConsumerConfigProvider)),",
				},
			},
		}
	}
	return nil
}

// patchFile reads a file, applies all patches in order, and writes it back.
func patchFile(path string, patches []patch) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	for _, p := range patches {
		if strings.Contains(content, p.sentinel) {
			continue // already applied
		}
		idx := strings.Index(content, p.find)
		if idx == -1 {
			continue // anchor not found — skip silently (file may differ from template)
		}
		if p.insertAfter {
			insertAt := idx + len(p.find)
			content = content[:insertAt] + p.code + content[insertAt:]
		} else {
			// replace the find string with code (used when we rewrite the struct header)
			content = content[:idx] + p.code + content[idx+len(p.find):]
		}
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

// ── delivery/fx.go patching ───────────────────────────────────────────────────

func patchDeliveryFx(root string, data *wizard.ProjectData, svc string) error {
	fxPath := filepath.Join(root, "internals", "delivery", "fx.go")
	raw, err := os.ReadFile(fxPath)
	if err != nil {
		return err
	}
	content := string(raw)

	type importSpec struct{ sentinel, line string }
	type fnSpec struct{ sentinel, body string }

	var imports []importSpec
	var fn fnSpec

	switch svc {
	case "http":
		imports = []importSpec{
			{
				sentinel: data.ModuleName + "/internals/delivery/http\"",
				line:     "\tdeliveryHttp \"" + data.ModuleName + "/internals/delivery/http\"\n",
			},
			{
				sentinel: data.ModuleName + "/internals/delivery/http/impl\"",
				line:     "\timplHttp \"" + data.ModuleName + "/internals/delivery/http/impl\"\n",
			},
		}
		fn = fnSpec{
			sentinel: "ModuleHttp",
			body: `
func ModuleHttp() fx.Option {
	return fx.Module("delivery/http",
		fx.Provide(fx.Private, implHttp.NewHandler),
		fx.Provide(deliveryHttp.NewRouter),
	)
}
`,
		}

	case "grpc":
		imports = []importSpec{
			{
				sentinel: `"google.golang.org/grpc"`,
				line:     "\t\"google.golang.org/grpc\"\n",
			},
			{
				sentinel: `serverGrpc "github.com/triasbrata/higo-framework/server/grpc"`,
				line:     "\tserverGrpc \"github.com/triasbrata/higo-framework/server/grpc\"\n",
			},
		}
		fn = fnSpec{
			sentinel: "ModuleGrpc",
			body: `
func ModuleGrpc() fx.Option {
	return fx.Module("delivery/grpc",
		fx.Provide(func() serverGrpc.GrpcServerBinding {
			return func(s *grpc.Server) {
				// Register your gRPC services here.
			}
		}),
	)
}
`,
		}

	case "consumer":
		imports = []importSpec{
			{
				sentinel: data.ModuleName + "/internals/delivery/consumer\"",
				line:     "\t\"" + data.ModuleName + "/internals/delivery/consumer\"\n",
			},
			{
				sentinel: data.ModuleName + "/internals/delivery/consumer/impl\"",
				line:     "\timplConsumer \"" + data.ModuleName + "/internals/delivery/consumer/impl\"\n",
			},
			{
				sentinel: `cmr "github.com/triasbrata/higo-framework/messagebroker/consumer"`,
				line:     "\tcmr \"github.com/triasbrata/higo-framework/messagebroker/consumer\"\n",
			},
			{
				sentinel: `serverConsumer "github.com/triasbrata/higo-framework/server/consumer"`,
				line:     "\tserverConsumer \"github.com/triasbrata/higo-framework/server/consumer\"\n",
			},
		}
		fn = fnSpec{
			sentinel: "ModuleConsumer",
			body: `
func ModuleConsumer() fx.Option {
	return fx.Module("delivery/consumer",
		fx.Provide(implConsumer.NewHandlerConsumer),
		fx.Provide(func(handler consumer.ConsumerHandler) serverConsumer.ConsumerRouting {
			return func(builder cmr.ConsumerBuilder) {
				consumer.NewRoutingConsumer(handler, builder)
			}
		}),
	)
}
`,
		}
	}

	// apply imports
	for _, imp := range imports {
		if !strings.Contains(content, imp.sentinel) {
			if idx := strings.Index(content, "import (\n"); idx != -1 {
				insertAt := idx + len("import (\n")
				content = content[:insertAt] + imp.line + content[insertAt:]
			}
		}
	}

	// append function if missing
	if !strings.Contains(content, fn.sentinel) {
		content += fn.body
	}

	return os.WriteFile(fxPath, []byte(content), 0o644)
}

// ── delivery scaffold inline templates ────────────────────────────────────────

const httpHandlerTmpl = `package http

import "github.com/gofiber/fiber/v2"

// Handler defines the HTTP handler interface.
// Add your handler methods here and implement them in impl/.
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
