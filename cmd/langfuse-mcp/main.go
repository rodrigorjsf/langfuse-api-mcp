// Command langfuse-mcp is the MCP server for the Langfuse public API.
//
// This package only wires modules together (ADR-0009): capture the ambient CA
// variables, load config, build the trust pool, then (in later milestones) the
// Langfuse client and a transport.
// For now the executable builds the trust pool, logs its CA sources and exits.
package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func main() {
	// First, before anything touches certificate handling: Go would let
	// SSL_CERT_FILE/SSL_CERT_DIR replace the OS roots (ADR-0006).
	ambient, err := trust.CaptureAmbient()
	// Logs go to stderr: stdout is reserved for the stdio transport.
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	if !run(log, os.Environ(), ambient) {
		os.Exit(1)
	}
}

// run starts the server with the given environment ("KEY=value" entries) and
// the ambient CA sources captured at process start. It reports whether startup
// succeeded; on failure it has logged one error line.
func run(log *slog.Logger, environ []string, ambient []trust.Source) bool {
	cfg, err := config.Load(envMap(environ))
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return false
	}

	_, report, err := trust.Build(trustSources(cfg, ambient))
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return false
	}
	for _, s := range report.Sources {
		if s.Warning != "" {
			log.Warn("CA source not fully loaded", "variable", s.Variable, "path", s.Path, "warning", s.Warning)
		}
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

// trustSources maps the configured and ambient CA paths to the trust module's sources.
func trustSources(cfg config.Config, ambient []trust.Source) trust.Sources {
	src := trust.Sources{Ambient: ambient, IgnoreAmbient: cfg.IgnoreAmbientCA}
	if cfg.CACert != "" {
		src.Explicit = append(src.Explicit, trust.Source{Variable: config.EnvCACert, Path: cfg.CACert})
	}
	if cfg.CACertsPath != "" {
		src.Explicit = append(src.Explicit, trust.Source{
			Variable: config.EnvCACertsPath, Path: cfg.CACertsPath, Directory: true,
		})
	}
	return src
}
