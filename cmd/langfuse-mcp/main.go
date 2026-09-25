// Command langfuse-mcp is the MCP server for the Langfuse public API.
//
// This package only wires modules together (ADR-0009): capture the ambient CA
// variables, load config (environment, then the optional config file), build
// the trust pool, then (in later milestones) the Langfuse client and a transport.
// For now the executable builds the trust pool, logs its CA sources and exits.
package main

import (
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func main() {
	// Nothing may run here before start: see start.
	if _, ok := start(); !ok {
		os.Exit(1)
	}
	// Later milestones build the Langfuse client with the trust pool and run a transport.
}

// start starts the server and returns its trust pool. Its first statement
// captures the ambient CA variables, before anything touches certificate
// handling: Go would let SSL_CERT_FILE/SSL_CERT_DIR replace the OS roots, and
// it caches the OS roots the first time they are loaded (ADR-0006). The
// process-level trust test (trustproof_test.go) fails if this order breaks.
// It reports whether startup succeeded; on failure it has logged one error line.
func start() (trust.Pool, bool) {
	ambient, err := trust.CaptureAmbient()
	// Logs go to stderr: stdout is reserved for the stdio transport.
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	var pool trust.Pool
	if err == nil {
		pool, err = startWith(log, os.Environ(), ambient)
	}
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return trust.Pool{}, false
	}
	return pool, true
}

// startWith is start after the capture: it never touches the process
// environment, only the given entries ("KEY=value") and the ambient CA sources
// captured at process start, and returns the trust pool.
func startWith(log *slog.Logger, environ []string, ambient []trust.Source) (trust.Pool, error) {
	file, err := config.ReadFile()
	if err != nil {
		return trust.Pool{}, err
	}
	cfg, err := config.Load(envMap(environ), file)
	if err != nil {
		return trust.Pool{}, err
	}

	pool, report, err := trust.Build(trustSources(cfg, slices.Concat(ambient, ambientInFile(cfg, ambient))))
	if err != nil {
		return trust.Pool{}, err
	}
	for _, s := range report.Sources {
		if s.Warning != "" {
			log.Warn("CA source not fully loaded", "variable", s.Variable, "path", s.Path, "warning", s.Warning)
		}
	}
	log.Info("CA sources loaded", "roots", report.Roots, "sources", report.Sources)
	return pool, nil
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

// ambientInFile returns the ambient CA sources set in the config file, except
// those whose variable the environment already set (captured): the environment
// wins, per variable. They stay ambient sources (warn and skip, ignorable);
// whether they should count as explicit is open in #29.
//
// This precedence would belong in config, but config.Load never sees the
// environment's ambient values: start must capture and unset
// SSL_CERT_FILE/SSL_CERT_DIR before config loads (ADR-0006), so only the
// captured sources know which variables the environment set. It is the one
// piece of settings logic cmd owns.
func ambientInFile(cfg config.Config, captured []trust.Source) []trust.Source {
	return trust.AmbientSources(func(variable string) string {
		if slices.ContainsFunc(captured, func(s trust.Source) bool { return s.Variable == variable }) {
			return ""
		}
		return cfg.AmbientInFile.Lookup(variable)
	}, string(config.OriginConfigFile))
}

// trustSources maps the configured and ambient CA paths to the trust module's
// sources. Paths from the config file are explicit sources, like those from the
// environment.
func trustSources(cfg config.Config, ambient []trust.Source) trust.Sources {
	src := trust.Sources{Ambient: ambient, IgnoreAmbient: cfg.IgnoreAmbientCA}
	for _, e := range []struct {
		variable  string
		setting   config.Setting
		directory bool
	}{
		{config.EnvCACert, cfg.CACert, false},
		{config.EnvCACertsPath, cfg.CACertsPath, true},
	} {
		if e.setting.Value != "" {
			src.Explicit = append(src.Explicit, trust.Source{
				Variable: e.variable, Path: e.setting.Value, Directory: e.directory, Origin: string(e.setting.Origin),
			})
		}
	}
	return src
}
