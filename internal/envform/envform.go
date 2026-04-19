package envform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

type EnvValues struct {
	AppName     string
	OtelEndpoint string
	OtelUseGRPC  bool

	LogAddSource bool
	LogLevel     string

	HTTPPort    string
	HTTPAddress string

	GRPCPort    string
	GRPCAddress string

	AmqpURI            string
	AmqpConnectionName string

	PyroscopeEnabled       bool
	PyroscopeServerAddress string
}

func defaults(data *wizard.ProjectData) EnvValues {
	return EnvValues{
		AppName:                data.AppName,
		OtelEndpoint:           "localhost:4317",
		OtelUseGRPC:            true,
		LogAddSource:           false,
		LogLevel:               "INFO",
		HTTPPort:               "8000",
		HTTPAddress:            "",
		GRPCPort:               "8001",
		GRPCAddress:            "",
		AmqpURI:                "amqp://guest:guest@localhost:5672",
		AmqpConnectionName:     data.AppName + "-consumer",
		PyroscopeEnabled:       false,
		PyroscopeServerAddress: "http://localhost:9999",
	}
}

// Run asks the user whether to create a .env file and, if yes, collects values via a form.
func Run(data *wizard.ProjectData, outDir string) error {
	var wantEnv bool
	if err := huh.NewConfirm().
		Title("Create .env file now?").
		Description("You can always create it later from .env.example").
		Affirmative("Yes").
		Negative("No").
		Value(&wantEnv).
		Run(); err != nil {
		return err
	}
	if !wantEnv {
		return nil
	}

	v := defaults(data)
	groups := buildGroups(data, &v)

	if err := huh.NewForm(groups...).Run(); err != nil {
		return err
	}

	content := render(data, &v)
	envPath := filepath.Join(outDir, ".env")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	return nil
}

func buildGroups(data *wizard.ProjectData, v *EnvValues) []*huh.Group {
	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewInput().
				Title("App name").
				Value(&v.AppName),
			huh.NewInput().
				Title("OTel endpoint").
				Description("OTLP collector address (e.g. localhost:4317)").
				Value(&v.OtelEndpoint),
			huh.NewConfirm().
				Title("Use gRPC transport for OTel?").
				Affirmative("gRPC").
				Negative("HTTP").
				Value(&v.OtelUseGRPC),
		).Title("General"),

		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Log level").
				Options(
					huh.NewOption("DEBUG", "DEBUG"),
					huh.NewOption("INFO", "INFO"),
					huh.NewOption("WARN", "WARN"),
					huh.NewOption("ERROR", "ERROR"),
				).
				Value(&v.LogLevel),
			huh.NewConfirm().
				Title("Include source location in logs?").
				Description("Adds file and line number to every log entry (LOG_ADD_SOURCE)").
				Affirmative("Yes").
				Negative("No").
				Value(&v.LogAddSource),
		).Title("Logging"),
	}

	if data.HasHTTP {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title("HTTP port").Value(&v.HTTPPort),
			huh.NewInput().Title("HTTP bind address").Description("Leave empty to bind on all interfaces").Value(&v.HTTPAddress),
		).Title("HTTP Server"))
	}

	if data.HasGRPC {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title("gRPC port").Value(&v.GRPCPort),
			huh.NewInput().Title("gRPC bind address").Description("Leave empty to bind on all interfaces").Value(&v.GRPCAddress),
		).Title("gRPC Server"))
	}

	if data.HasConsumer {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title("AMQP URI").Value(&v.AmqpURI),
			huh.NewInput().Title("AMQP connection name").Value(&v.AmqpConnectionName),
		).Title("RabbitMQ"))
	}

	if data.HasPyroscope {
		groups = append(groups, huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Pyroscope profiling?").
				Affirmative("Yes").
				Negative("No").
				Value(&v.PyroscopeEnabled),
			huh.NewInput().Title("Pyroscope server address").Value(&v.PyroscopeServerAddress),
		).Title("Pyroscope"))
	}

	return groups
}

func render(data *wizard.ProjectData, v *EnvValues) string {
	boolStr := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}

	var sb strings.Builder
	line := func(k, val string) { fmt.Fprintf(&sb, "%s=%s\n", k, val) }
	comment := func(s string) { fmt.Fprintf(&sb, "\n# %s\n", s) }

	line("APP_NAME", v.AppName)
	line("GIT_COMMIT_ID", "dev")

	comment("Logging")
	line("LOG_LEVEL", v.LogLevel)
	line("LOG_ADD_SOURCE", boolStr(v.LogAddSource))

	comment("OpenTelemetry")
	line("OTEL_ENDPOINT", v.OtelEndpoint)
	line("OTEL_USE_GRPC", boolStr(v.OtelUseGRPC))

	if data.HasHTTP {
		comment("HTTP Server")
		line("HTTP_PORT", v.HTTPPort)
		line("HTTP_ADDRESS", v.HTTPAddress)
	}
	if data.HasGRPC {
		comment("gRPC Server")
		line("GRPC_PORT", v.GRPCPort)
		line("GRPC_ADDRESS", v.GRPCAddress)
	}
	if data.HasConsumer {
		comment("RabbitMQ")
		line("AMQP_URI", v.AmqpURI)
		line("AMQP_CONNECTION_NAME", v.AmqpConnectionName)
	}
	if data.HasPyroscope {
		comment("Pyroscope Profiling")
		line("PYROSCOPE_ENABLED", boolStr(v.PyroscopeEnabled))
		line("PYROSCOPE_SERVER_ADDRESS", v.PyroscopeServerAddress)
	}

	return sb.String()
}
