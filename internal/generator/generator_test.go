package generator_test

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/triasbrata/higo-cli/internal/generator"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

type testCase struct {
	data     wizard.ProjectData
	httpPort int // 0 = no HTTP
	grpcPort int // 0 = no gRPC
	needAMQP bool
}

// Each case gets unique ports to allow parallel execution without conflicts.
var matrix = []testCase{
	{wizard.ProjectData{ProjectName: "test-http", ModuleName: "github.com/test/test-http", AppName: "test-http", HasHTTP: true}, 18000, 0, false},
	{wizard.ProjectData{ProjectName: "test-grpc", ModuleName: "github.com/test/test-grpc", AppName: "test-grpc", HasGRPC: true}, 0, 19001, false},
	{wizard.ProjectData{ProjectName: "test-consumer", ModuleName: "github.com/test/test-consumer", AppName: "test-consumer", HasConsumer: true}, 0, 0, true},
	{wizard.ProjectData{ProjectName: "test-mix-hg", ModuleName: "github.com/test/test-mix-hg", AppName: "test-mix-hg", HasHTTP: true, HasGRPC: true, HasMix: true}, 18003, 19003, false},
	{wizard.ProjectData{ProjectName: "test-mix-hc", ModuleName: "github.com/test/test-mix-hc", AppName: "test-mix-hc", HasHTTP: true, HasConsumer: true, HasMix: true}, 18004, 0, true},
	{wizard.ProjectData{ProjectName: "test-mix-gc", ModuleName: "github.com/test/test-mix-gc", AppName: "test-mix-gc", HasGRPC: true, HasConsumer: true, HasMix: true}, 0, 19005, true},
	{wizard.ProjectData{ProjectName: "test-mix-all", ModuleName: "github.com/test/test-mix-all", AppName: "test-mix-all", HasHTTP: true, HasGRPC: true, HasConsumer: true, HasMix: true}, 18006, 19006, true},
	{wizard.ProjectData{ProjectName: "test-pyroscope", ModuleName: "github.com/test/test-pyroscope", AppName: "test-pyroscope", HasHTTP: true, HasPyroscope: true}, 18007, 0, false},
}

func amqpAvailable() bool {
	conn, err := net.DialTimeout("tcp", "localhost:5672", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func scaffold(t *testing.T, outDir string, data *wizard.ProjectData) {
	t.Helper()
	require.NoError(t, generator.Generate(outDir, data), "generate")

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
}

func TestBoilerplateBuilds(t *testing.T) {
	// use os.MkdirTemp so we control cleanup (leave dir on failure for inspection)
	base, err := os.MkdirTemp("", "higo-build-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		if !t.Failed() {
			os.RemoveAll(base)
		} else {
			t.Logf("artifacts left for inspection: %s", base)
		}
	})

	for _, tc := range matrix {
		t.Run(tc.data.ProjectName, func(t *testing.T) {
			t.Parallel()

			outDir := filepath.Join(base, tc.data.ProjectName)
			scaffold(t, outDir, &tc.data)

			build := exec.Command("go", "build", "./...")
			build.Dir = outDir
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build failed:\n%s", out)
			}

			vet := exec.Command("go", "vet", "./...")
			vet.Dir = outDir
			if out, err := vet.CombinedOutput(); err != nil {
				t.Fatalf("go vet failed:\n%s", out)
			}
		})
	}
}

func TestBoilerplateRuns(t *testing.T) {
	base, err := os.MkdirTemp("", "higo-run-*")
	require.NoError(t, err)
	t.Cleanup(func() {
		if !t.Failed() {
			os.RemoveAll(base)
		} else {
			t.Logf("artifacts left for inspection: %s", base)
		}
	})

	hasAMQP := amqpAvailable()

	for _, tc := range matrix {
		t.Run(tc.data.ProjectName, func(t *testing.T) {
			t.Parallel()

			if tc.needAMQP && !hasAMQP {
				t.Skip("RabbitMQ not available on localhost:5672")
			}

			outDir := filepath.Join(base, tc.data.ProjectName)
			scaffold(t, outDir, &tc.data)

			srcPkg, binName := entrypoint(&tc.data)
			binPath := filepath.Join(outDir, binName)
			build := exec.Command("go", "build", "-o", binPath, srcPkg)
			build.Dir = outDir
			if out, err := build.CombinedOutput(); err != nil {
				t.Fatalf("go build failed:\n%s", out)
			}

			env := append(os.Environ(),
				"APP_NAME="+tc.data.AppName,
				"OTEL_ENDPOINT=127.0.0.1:9999", // unreachable — OTLP gRPC dials lazily, won't block startup
				"PYROSCOPE_ENABLED=false",
			)
			if tc.httpPort != 0 {
				env = append(env, fmt.Sprintf("HTTP_PORT=%d", tc.httpPort))
			}
			if tc.grpcPort != 0 {
				env = append(env, fmt.Sprintf("GRPC_PORT=%d", tc.grpcPort))
			}

			var output bytes.Buffer
			proc := exec.Command(binPath)
			proc.Env = env
			proc.Dir = outDir
			proc.Stdout = &output
			proc.Stderr = &output
			require.NoError(t, proc.Start(), "start binary")

			t.Cleanup(func() {
				if proc.Process != nil {
					_ = proc.Process.Signal(syscall.SIGTERM)
					done := make(chan struct{})
					go func() { proc.Wait(); close(done) }()
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						proc.Process.Kill()
					}
				}
				if t.Failed() {
					t.Logf("process output:\n%s", output.String())
				}
			})

			if tc.httpPort != 0 {
				waitPort(t, tc.httpPort, 15*time.Second, "HTTP")
				callHi(t, tc.httpPort)
			}
			if tc.grpcPort != 0 {
				waitPort(t, tc.grpcPort, 15*time.Second, "gRPC")
			}
			if tc.httpPort == 0 && tc.grpcPort == 0 {
				time.Sleep(3 * time.Second)
				if proc.ProcessState != nil && proc.ProcessState.Exited() {
					t.Fatalf("process exited early — fx container likely failed\noutput:\n%s", output.String())
				}
			}
		})
	}
}

func waitPort(t *testing.T, port int, timeout time.Duration, label string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("%s server never started on port %d within %s", label, port, timeout)
}

func callHi(t *testing.T, port int) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/hi?name=Test", port)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode, "GET /hi returned unexpected status")
			return
		}
		lastErr = err
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("GET /hi never succeeded within 10s: %v", lastErr)
}

func entrypoint(data *wizard.ProjectData) (srcPkg, binName string) {
	switch {
	case data.HasMix:
		return "./cmd/mix", "mix_service"
	case data.HasHTTP:
		return "./cmd/http", "http_service"
	case data.HasGRPC:
		return "./cmd/grpc", "grpc_service"
	default:
		return "./cmd/consumer", "consumer_service"
	}
}
