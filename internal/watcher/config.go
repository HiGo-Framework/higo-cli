package watcher

import (
	"fmt"
	"time"

	"github.com/triasbrata/higo-cli/internal/logviewer"
)

var defaultExclude = []string{
	"tmp", ".git", "vendor", "node_modules", "testdata", ".claude", "gen",
}

type ServiceType string

const (
	ServiceHTTP     ServiceType = "http"
	ServiceGRPC     ServiceType = "grpc"
	ServiceConsumer ServiceType = "consumer"
	ServiceMix      ServiceType = "mix"
)

type Config struct {
	Service    ServiceType
	RootDir    string
	Exclude    []string
	BuildDelay time.Duration
	// LogCh receives all log entries (app + system). Closed by the watcher when it exits.
	LogCh   chan<- logviewer.Entry
	LogFile string // absolute path of the session log file
}

func (c *Config) applyDefaults() {
	if c.RootDir == "" {
		c.RootDir = "."
	}
	if c.BuildDelay == 0 {
		c.BuildDelay = time.Second
	}
	seen := map[string]bool{}
	merged := make([]string, 0, len(defaultExclude)+len(c.Exclude))
	for _, e := range append(defaultExclude, c.Exclude...) {
		if !seen[e] {
			seen[e] = true
			merged = append(merged, e)
		}
	}
	c.Exclude = merged
}

func (c *Config) buildArgs() (srcPkg string, outBin string) {
	switch c.Service {
	case ServiceGRPC:
		return "./cmd/grpc", "tmp/grpc_service"
	case ServiceConsumer:
		return "./cmd/consumer", "tmp/consumer_service"
	case ServiceMix:
		return "./cmd/mix", "tmp/mix_service"
	default:
		return "./cmd/http", "tmp/http_service"
	}
}

func (c *Config) envAppName() string {
	return fmt.Sprintf("APP_NAME=%s-%s", "app", c.Service)
}
