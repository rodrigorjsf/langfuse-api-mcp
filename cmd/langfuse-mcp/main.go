// Command langfuse-mcp is the MCP server for the Langfuse public API.
//
// This package only wires modules together (ADR-0009): load config (environment,
// then the optional config file), build the
// trust pool, then (in later milestones) the Langfuse client and a transport.
// For now the executable builds the trust pool, logs its CA sources and exits.
package main

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func main() {
	if !run(os.Environ(), os.Stderr) {
		os.Exit(1)
	}
}

// run starts the server with the given environment ("KEY=value" entries) and
// logs to stderr (stdout is reserved for the stdio transport). It reports
// whether startup succeeded; on failure it has logged one error line.
func run(environ []string, stderr io.Writer) bool {
	log := slog.New(slog.NewJSONHandler(stderr, nil))
	file, err := config.ReadFile()
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return false
	}
	cfg, err := config.Load(envMap(environ), file)
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return false
	}

	_, report, err := trust.Build(trustSources(cfg))
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return false
	}
	log.Info("CA sources loaded", "roots", report.Roots, "sources", report.Sources)
	return true
}

// envMap turns "KEY=value" entries into a map; the last entry for a key wins.
func envMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// trustSources maps the configured CA paths to the trust module's sources.
// Paths from the config file are explicit sources, like those from the environment.
func trustSources(cfg config.Config) trust.Sources {
	var src trust.Sources
	if cfg.CACert.Value != "" {
		src.Explicit = append(src.Explicit, trust.Source{
			Variable: config.EnvCACert, Path: cfg.CACert.Value, Origin: string(cfg.CACert.Origin),
		})
	}
	if cfg.CACertsPath.Value != "" {
		src.Explicit = append(src.Explicit, trust.Source{
			Variable: config.EnvCACertsPath, Path: cfg.CACertsPath.Value, Directory: true,
			Origin: string(cfg.CACertsPath.Origin),
		})
	}
	return src
}
