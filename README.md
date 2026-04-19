# higo-cli

CLI tool for [higo-framework](https://github.com/triasbrata/higo-framework) — scaffold new projects and run services with hot reload.

## Install

```bash
go install github.com/triasbrata/higo-cli@latest
```

## Commands

### `higo init`

Interactive wizard to scaffold a new project. Prompts for project name, module path, and which servers to include (HTTP, gRPC, RabbitMQ consumer, or any combination).

```bash
higo init
```

Generated project structure:

```
my-app/
├── cmd/
│   ├── http/          # HTTP entrypoint
│   ├── grpc/          # gRPC entrypoint
│   ├── consumer/      # RabbitMQ consumer entrypoint
│   └── mix/           # Combined entrypoint (when multiple servers selected)
├── internals/
│   ├── bootstrap/     # fx app wiring per service type
│   ├── config/        # Config structs + env loader
│   ├── delivery/      # Handlers (http, grpc, consumer)
│   ├── entities/      # Domain models
│   └── service/       # Business logic
├── .env.example
└── go.mod
```

After scaffolding, the wizard optionally walks you through creating a `.env` file interactively.

### `higo start [http|grpc|consumer|mix]`

Start a service with hot reload. Watches all `.go` files and rebuilds + restarts on change (1s debounce by default).

```bash
# auto-detect service from cmd/ layout
higo start

# explicit service
higo start http
higo start grpc
higo start mix

# options
higo start http --exclude gen,proto --delay 500ms
```

When no service is specified, `higo start` auto-detects:
- runs `cmd/mix` if it exists
- runs the single available `cmd/<service>` otherwise
- errors if multiple services exist without a `cmd/mix`

## Environment variables (generated projects)

| Variable | Default | Description |
|---|---|---|
| `APP_NAME` | project name | Service name used in traces and logs |
| `LOG_LEVEL` | `INFO` | Minimum log level (`DEBUG`/`INFO`/`WARN`/`ERROR`) |
| `LOG_ADD_SOURCE` | `false` | Include file and line number in log entries |
| `OTEL_ENDPOINT` | `localhost:4317` | OTLP collector address |
| `OTEL_USE_GRPC` | `true` | Use gRPC transport for OTLP (false = HTTP) |
| `HTTP_PORT` | `8000` | HTTP server port |
| `GRPC_PORT` | `8001` | gRPC server port |
| `AMQP_URI` | `amqp://guest:guest@localhost:5672` | RabbitMQ connection URI |
| `PYROSCOPE_ENABLED` | `false` | Enable Pyroscope continuous profiling |
| `PYROSCOPE_SERVER_ADDRESS` | `http://localhost:9999` | Pyroscope server URL |

## Requirements

- Go 1.24+
- [Task](https://taskfile.dev) (optional, for generated project shortcuts)
