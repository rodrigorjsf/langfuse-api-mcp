package config_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

func TestLoadReadsExplicitCASourcePathsFromTheEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(map[string]string{
		"LANGFUSE_CA_CERT":       "/etc/corp/root.pem",
		"LANGFUSE_CA_CERTS_PATH": "/etc/corp/certs",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := config.Config{CACert: "/etc/corp/root.pem", CACertsPath: "/etc/corp/certs"}
	if cfg != want {
		t.Fatalf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadLeavesExplicitCASourcesUnsetWhenTheVariablesAreAbsentOrEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]string{
		"absent": {},
		"empty":  {"LANGFUSE_CA_CERT": "", "LANGFUSE_CA_CERTS_PATH": ""},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if cfg, err := config.Load(env); err != nil || cfg != (config.Config{}) {
				t.Fatalf("Load() = %+v, %v; want no CA sources and no error", cfg, err)
			}
		})
	}
}

func TestLoadReadsTheIgnoreAmbientCAFlag(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{"": false, "false": false, "true": true, "TRUE": true, "1": true, "0": false}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(map[string]string{"LANGFUSE_MCP_IGNORE_AMBIENT_CA": value})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.IgnoreAmbientCA != want {
				t.Fatalf("IgnoreAmbientCA = %v for %q, want %v", cfg.IgnoreAmbientCA, value, want)
			}
		})
	}
}

func TestLoadFailsNamingTheVariableOfAnInvalidIgnoreAmbientCAFlag(t *testing.T) {
	t.Parallel()

	_, err := config.Load(map[string]string{"LANGFUSE_MCP_IGNORE_AMBIENT_CA": "yes"})

	if err == nil || !strings.Contains(err.Error(), "LANGFUSE_MCP_IGNORE_AMBIENT_CA") {
		t.Fatalf("Load error = %v, want an error naming LANGFUSE_MCP_IGNORE_AMBIENT_CA", err)
	}
}

// FuzzLoad checks that Load never panics and either fails or returns a flag
// consistent with the input (go.md: fuzz every config parser).
func FuzzLoad(f *testing.F) {
	for _, seed := range []string{"", "true", "false", "1", "yes", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		cfg, err := config.Load(map[string]string{"LANGFUSE_MCP_IGNORE_AMBIENT_CA": value})
		if err != nil && cfg != (config.Config{}) {
			t.Fatalf("Load(%q) returned a config %+v together with error %v", value, cfg, err)
		}
		if value == "" && (err != nil || cfg.IgnoreAmbientCA) {
			t.Fatalf("Load(\"\") = %+v, %v; want the flag unset", cfg, err)
		}
	})
}
