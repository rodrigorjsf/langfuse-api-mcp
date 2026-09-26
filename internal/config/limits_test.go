package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

// Issue #37: the operator bounds the rate and concurrency of Langfuse calls.

func TestLoadReadsTheRateLimitAndMaxConcurrencyFromTheEnvironment(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{
		"LANGFUSE_MCP_RATE_LIMIT":      "600",
		"LANGFUSE_MCP_MAX_CONCURRENCY": "8",
	}, config.File{})

	if cfg.RateLimit != 600 || cfg.MaxConcurrency != 8 {
		t.Fatalf("RateLimit, MaxConcurrency = %d, %d; want 600, 8", cfg.RateLimit, cfg.MaxConcurrency)
	}
}

func TestLoadLeavesTheMaxConcurrencyUnsetSoTheClientDefaultApplies(t *testing.T) {
	t.Parallel()

	cfg, err := load(map[string]string{"LANGFUSE_MCP_MAX_CONCURRENCY": ""}, config.File{})

	if err != nil || cfg.MaxConcurrency != 0 {
		t.Fatalf("MaxConcurrency, err = %d, %v; want 0 (unset), nil", cfg.MaxConcurrency, err)
	}
}

func TestLoadReadsTheLimitsFromTheConfigFileAndLetsTheEnvironmentWin(t *testing.T) {
	t.Parallel()
	file := configFile("LANGFUSE_MCP_RATE_LIMIT=100\nLANGFUSE_MCP_MAX_CONCURRENCY=2\n")

	cfg := mustLoad(t, map[string]string{"LANGFUSE_MCP_MAX_CONCURRENCY": "16"}, file)

	if cfg.RateLimit != 100 || cfg.MaxConcurrency != 16 {
		t.Fatalf("RateLimit, MaxConcurrency = %d, %d; want 100 (file), 16 (environment)", cfg.RateLimit, cfg.MaxConcurrency)
	}
}

func TestLoadAcceptsTheLimitsAtTheEdgesOfTheirRanges(t *testing.T) {
	t.Parallel()

	for _, env := range []map[string]string{
		{"LANGFUSE_MCP_RATE_LIMIT": "1", "LANGFUSE_MCP_MAX_CONCURRENCY": "1"},
		{"LANGFUSE_MCP_RATE_LIMIT": "60000", "LANGFUSE_MCP_MAX_CONCURRENCY": "64"},
	} {
		if _, err := load(env, config.File{}); err != nil {
			t.Fatalf("Load(%v): %v, want accepted", env, err)
		}
	}
}

// Dangerous parameters: a zero, negative, non-numeric or absurd limit stops
// startup, naming the variable; a limit is never clamped.
func TestLoadRefusesAnInvalidLimitNamingTheVariable(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"zero":            "0",
		"negative":        "-5",
		"non-numeric":     "fast",
		"fraction":        "1.5",
		"absurdly large":  "99999999999999999999999",
		"above the range": "1000000",
		"control char":    "10\x00",
	}
	for _, variable := range []string{"LANGFUSE_MCP_RATE_LIMIT", "LANGFUSE_MCP_MAX_CONCURRENCY"} {
		for name, value := range values {
			t.Run(variable+"/"+name, func(t *testing.T) {
				t.Parallel()

				cfg, err := load(map[string]string{variable: value}, config.File{})

				if err == nil {
					t.Fatalf("Load(%s=%q) = %+v, want a startup error", variable, value, cfg)
				}
				if !strings.Contains(err.Error(), variable) {
					t.Fatalf("error %q does not name %s", err, variable)
				}
			})
		}
	}
}

func TestLoadRefusesAnInvalidLimitInTheConfigFileWithoutQuotingIt(t *testing.T) {
	t.Parallel()
	file := configFile("LANGFUSE_MCP_RATE_LIMIT=sk-lf-typo\n")

	_, err := load(map[string]string{}, file)

	if err == nil {
		t.Fatal("Load() succeeded on an invalid rate limit in the config file")
	}
	msg := err.Error()
	if !strings.Contains(msg, file.Path) || !strings.Contains(msg, "LANGFUSE_MCP_RATE_LIMIT") {
		t.Fatalf("error %q does not name %s and the variable", msg, file.Path)
	}
	if strings.Contains(msg, "sk-lf-typo") {
		t.Fatalf("error %q quotes the config file value", msg)
	}
}

func TestLoadDoesNotWarnAboutTheLimitKeysInTheConfigFile(t *testing.T) {
	t.Parallel()

	_, ignored, err := config.Load(connectionEnv(nil),
		configFile("LANGFUSE_MCP_RATE_LIMIT=30\nLANGFUSE_MCP_MAX_CONCURRENCY=4\n"))

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ignored) != 0 {
		t.Fatalf("ignored keys = %+v, want none", ignored)
	}
}

// FuzzLoadLimits checks that Load never panics on a limit value and either
// refuses it or accepts exactly a whole number in range (go.md: fuzz every
// config parser).
func FuzzLoadLimits(f *testing.F) {
	for _, seed := range []string{"", "1", "30", "64", "65", "60000", "60001", "0", "-1", "+5", "1e3", "abc", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		cfg, err := load(map[string]string{"LANGFUSE_MCP_RATE_LIMIT": value, "LANGFUSE_MCP_MAX_CONCURRENCY": value}, config.File{})
		if value == "" { // unset: the Cloud host default (load's host) and the client's concurrency default
			if err != nil || cfg.RateLimit != 30 || cfg.MaxConcurrency != 0 {
				t.Fatalf("Load(\"\") = %d, %d, %v; want 30 (Cloud default), 0 (unset)", cfg.RateLimit, cfg.MaxConcurrency, err)
			}
			return
		}
		n, parseErr := strconv.Atoi(value)
		valid := parseErr == nil && n >= 1 && n <= 64 // 64 is the tighter of the two ranges
		if valid != (err == nil) {
			t.Fatalf("Load(%q) error = %v, want accepted: %v", value, err, valid)
		}
		if valid && (cfg.RateLimit != n || cfg.MaxConcurrency != n) {
			t.Fatalf("Load(%q) = %d, %d; want %d, %d", value, cfg.RateLimit, cfg.MaxConcurrency, n, n)
		}
	})
}

// Issue #47: with no explicit rate limit, the default follows the host: a
// Cloud host keeps the Cloud Hobby limit, any other host gets a generous one.

func TestLoadDefaultsTheRateLimitTo30OnEveryCloudHost(t *testing.T) {
	t.Parallel()
	for _, host := range []string{
		"https://cloud.langfuse.com",
		"https://us.cloud.langfuse.com",
		"https://jp.cloud.langfuse.com",
		"https://hipaa.cloud.langfuse.com",
		"https://CLOUD.Langfuse.COM",       // host names are case-insensitive
		"https://cloud.langfuse.com:443/",  // the port and path are ignored
		"https://us.cloud.langfuse.com/eu", // a path naming another region changes nothing
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			assertRateLimit(t, map[string]string{"LANGFUSE_BASE_URL": host}, config.File{}, 30, config.RateLimitDefaultCloud)
		})
	}
}

// assertRateLimit loads env and file and fails unless the effective rate limit
// and its source are want and wantSource.
func assertRateLimit(t *testing.T, env map[string]string, file config.File, want int, wantSource config.RateLimitSource) {
	t.Helper()
	cfg, err := load(env, file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RateLimit != want || cfg.RateLimitSource != wantSource {
		t.Fatalf("RateLimit, source = %d, %q; want %d, %q", cfg.RateLimit, cfg.RateLimitSource, want, wantSource)
	}
}

func TestLoadDefaultsTheRateLimitTo1000OnASelfHostedOrLoopbackHost(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"https://langfuse.corp.example", "http://localhost:3000", "http://127.0.0.1:3000"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			assertRateLimit(t, map[string]string{"LANGFUSE_BASE_URL": host}, config.File{}, 1000, config.RateLimitDefaultSelfHosted)
		})
	}
}

func TestLoadLetsAnExplicitRateLimitWinOnAnyHost(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"https://cloud.langfuse.com", "https://langfuse.corp.example"} {
		t.Run(host+"/environment", func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"LANGFUSE_BASE_URL": host, "LANGFUSE_MCP_RATE_LIMIT": "250"}
			assertRateLimit(t, env, configFile("LANGFUSE_MCP_RATE_LIMIT=90\n"), 250, config.RateLimitExplicit)
		})
		t.Run(host+"/config file", func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"LANGFUSE_BASE_URL": host}
			assertRateLimit(t, env, configFile("LANGFUSE_MCP_RATE_LIMIT=90\n"), 90, config.RateLimitExplicit)
		})
	}
}

// Dangerous parameters: the host is operator config, but a look-alike must
// never pass for a Cloud host by suffix, substring, userinfo, path, trailing
// dot or Unicode case folding.
func TestLoadDoesNotTreatALookAlikeHostAsACloudHost(t *testing.T) {
	t.Parallel()
	for _, host := range []string{
		"https://cloud.langfuse.com.evil.example",
		"https://evilcloud.langfuse.com",
		"https://evil.cloud.langfuse.com",
		"https://cloud.langfuse.com@evil.example",
		"https://user:pass@evil.example/cloud.langfuse.com",
		"https://evil.example/https://cloud.langfuse.com",
		"https://evil.example?host=cloud.langfuse.com",
		"https://evil.example#cloud.langfuse.com",
		"https://cloud.langfuse.com.",
		"https://cloud.langfuſe.com", // LATIN SMALL LETTER LONG S folds to "s" under Unicode rules
		"https://cloud-langfuse.com",
		"https://langfuse.com",
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			assertRateLimit(t, map[string]string{"LANGFUSE_BASE_URL": host}, config.File{}, 1000, config.RateLimitDefaultSelfHosted)
		})
	}
}
