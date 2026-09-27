// Command langfuse-mcp is the MCP server for the Langfuse public API.
//
// This package only wires modules together (ADR-0009): capture the ambient CA
// variables, load config (environment, then the optional config file), build
// the trust pool, the catalog, the Langfuse client and the MCP server, then
// serve it over stdio until the client closes stdin.
package main

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/transport"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/trust"
)

func main() {
	// Nothing may run here before start: see start.
	app, ok := start()
	if !ok {
		os.Exit(1)
	}
	if err := app.serve(context.Background()); err != nil {
		app.log.Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

// app is the started server: its trust pool and the function that serves it.
type app struct {
	log   *slog.Logger
	pool  trust.Pool
	serve func(context.Context) error
}

// start starts the server and returns its trust pool. Its first statement
// captures the ambient CA variables, before anything touches certificate
// handling: Go would let SSL_CERT_FILE/SSL_CERT_DIR replace the OS roots, and
// it caches the OS roots the first time they are loaded (ADR-0006). The
// process-level trust test (trustproof_test.go) fails if this order breaks.
// It reports whether startup succeeded; on failure it has logged one error line.
func start() (app, bool) {
	ambient, err := trust.CaptureAmbient()
	// Logs go to stderr: stdout is reserved for the stdio transport.
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	var a app
	if err == nil {
		a, err = startWith(log, os.Environ(), ambient)
	}
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		return app{}, false
	}
	return a, true
}

// startWith is start after the capture: it never touches the process
// environment, only the given entries ("KEY=value") and the ambient CA sources
// captured at process start, and returns the started server. It detects the
// deployment profile of the Langfuse it is configured for, which takes at most
// langfuse.DefaultDetectionBudget.
func startWith(log *slog.Logger, environ []string, ambient []trust.Source) (app, error) {
	file, err := config.ReadFile()
	if err != nil {
		return app{}, err
	}
	cfg, ignored, err := config.Load(envMap(environ), file)
	if err != nil {
		return app{}, err
	}
	for _, k := range ignored {
		// The key name only (config escaped it); the value may be a misfiled secret.
		log.Warn("config file key ignored: not a known setting", "file", file.Path, "line", k.Line, "key", k.Name)
	}

	pool, report, err := trust.Build(trustSources(cfg, ambient))
	if err != nil {
		return app{}, err
	}
	for _, s := range report.Sources {
		if s.Warning != "" {
			log.Warn("CA source not fully loaded", "variable", s.Variable, "path", s.Path, "warning", s.Warning)
		}
	}
	log.Info("CA sources loaded", "roots", report.Roots, "sources", report.Sources)

	cat, err := catalog.Load()
	if err != nil {
		return app{}, err
	}
	// Logged after the last startup step that returns an error: a failed startup logs one line only.
	log.Info("rate limit", "perMinute", cfg.RateLimit, "source", cfg.RateLimitSource)
	logProxy(log, cfg.Proxy)
	// The key pair leaves config.Secret straight into a langfuse.KeyPair,
	// which redacts itself as config.Secret does.
	keys := langfuse.NewKeyPair(cfg.Connection.PublicKey.Reveal(), cfg.Connection.SecretKey.Reveal())
	client := langfuse.New(langfuse.Options{
		Host: cfg.Connection.Host, Keys: keys, TLS: pool.TLSConfig(),
		// Operator settings only (config resolves the rate limit's host-based
		// default; zero concurrency keeps the client's); no tool argument reaches them.
		RateLimit: cfg.RateLimit, MaxConcurrency: cfg.MaxConcurrency,
		// The proxy variables config.Load validated and resolved over the
		// environment and the config file; no tool argument reaches them.
		Proxy: langfuse.ProxyFromSettings(cfg.ProxySettings.HTTPS.Reveal(), cfg.ProxySettings.HTTP.Reveal(), cfg.ProxySettings.NoProxy),
	})
	// The deployment profile (ADR-0012 §3), detected once: the tool set and
	// the operation set are then fixed until the process exits.
	detection := detectProfile(log, client)
	profile := detection.Profile
	resolved := cat.Resolve(catalogProfile(profile))
	logProfile(log, detection, len(resolved.Operations()))
	srv := server.New(resolved, client, log, server.Secrets{Keys: keys}, profile)
	serve := func(ctx context.Context) error {
		// On shutdown, close the keep-alive connections to Langfuse instead of
		// leaving them to the process exit.
		defer client.CloseIdleConnections()
		return transport.Stdio(ctx, srv)
	}
	return app{log: log, pool: pool, serve: serve}, nil
}

// detectProfile detects the deployment profile within
// langfuse.DefaultDetectionBudget, logs a version below the supported floor as
// unsupported, and logs one warning per probe that could not decide. The
// warnings hold fixed text only, never a Langfuse answer.
func detectProfile(log *slog.Logger, client *langfuse.Client) langfuse.Detection {
	d := client.DetectProfile(context.Background(), langfuse.DefaultDetectionBudget)
	if version, ok := d.Profile.KnownVersion(); ok && d.Unsupported {
		log.Warn("unsupported Langfuse version", "version", version,
			"reason", "below the supported floor 3.0.0: operations are filtered by version range alone; families are ignored")
	}
	for _, w := range d.Warnings {
		log.Warn("deployment profile probe undecided", "probe", w.Probe, "reason", w.Reason)
	}
	return d
}

// logProfile logs the detected version, the families on, those of them kept on
// without a deciding answer, whether the version is unsupported, and the number
// of operations the resolved catalog offers. An unsupported version lists no
// family: the catalog ignores them. The version is untrusted Langfuse text:
// only a plain version is logged.
func logProfile(log *slog.Logger, d langfuse.Detection, operations int) {
	p := d.Profile
	version, ok := p.KnownVersion()
	if !ok {
		version = "unknown"
	}
	families := make([]string, 0, len(p.Families))
	undecided := make([]string, 0, len(d.Undecided))
	if !d.Unsupported {
		for _, f := range catalog.AllFamilies() {
			if p.On(f) {
				families = append(families, string(f))
			}
		}
		for _, f := range d.Undecided {
			undecided = append(undecided, string(f))
		}
	}
	log.Info("deployment profile", "version", version, "families", families, "undecided", undecided,
		"unsupported", d.Unsupported, "operations", operations)
}

// logProxy logs the proxy in use as scheme://host:port with its variable and
// source, never its credentials, or that none is set; and whether NO_PROXY is set.
func logProxy(log *slog.Logger, p config.Proxy) {
	if p.Endpoint == "" {
		log.Info("proxy", "endpoint", "none", "noProxySet", p.NoProxy)
		return
	}
	log.Info("proxy", "endpoint", p.Endpoint, "variable", p.Variable, "source", p.Origin, "noProxySet", p.NoProxy)
}

// envMap turns "KEY=value" entries into a map; the last entry for a key wins.
// Keys keep their stored spelling, also on Windows, where names are
// case-insensitive: see #62.
func envMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// ambientInFile returns the CA sources named by the ambient variables set in
// the config file, except those whose variable the environment already set
// (captured): the environment wins, per variable. The caller makes them
// explicit sources: spec #7 counts every config-file CA path as explicit (#29).
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

// trustSources maps the configured CA paths and the ambient sources captured
// from the environment to the trust module's sources. Every path from the
// config file is an explicit source, ambient variable names included (#29):
// the operator wrote it there on purpose, so a broken one stops startup and
// the ignore flag does not drop it.
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
	src.Explicit = append(src.Explicit, ambientInFile(cfg, ambient)...)
	return src
}

// catalogProfile is the deployment profile as the catalog reads it. Both share
// the catalog's family type; only the version needs checking on the way.
func catalogProfile(p langfuse.DeploymentProfile) catalog.Profile {
	// Version is untrusted Langfuse text: only a plain version crosses.
	version, _ := p.KnownVersion()
	return catalog.Profile{Version: version, Families: p.Families}
}
