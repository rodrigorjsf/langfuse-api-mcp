package config_test

import (
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

// Spec #109, ticket #110: write mode is off unless the operator sets
// LANGFUSE_MCP_ALLOW_WRITES=true, in the environment or the config file.

func TestLoadLeavesWriteModeOffWhenLangfuseMCPAllowWritesIsUnset(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{"LANGFUSE_MCP_ALLOW_WRITES": ""}, config.File{})

	if cfg.AllowWrites.On || cfg.AllowWrites.Origin != "" {
		t.Fatalf("AllowWrites = %+v, want off with no origin", cfg.AllowWrites)
	}
}

func TestLoadReadsWriteModeFromTheEnvironment(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{"true": true, "false": false} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			cfg := mustLoad(t, map[string]string{"LANGFUSE_MCP_ALLOW_WRITES": value}, config.File{})

			if cfg.AllowWrites != (config.WriteMode{On: want, Origin: config.OriginEnvironment}) {
				t.Fatalf("AllowWrites = %+v for %q, want On %v from the environment", cfg.AllowWrites, value, want)
			}
		})
	}
}

func TestLoadReadsWriteModeFromTheConfigFile(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{}, configFile("LANGFUSE_MCP_ALLOW_WRITES=true\n"))

	if cfg.AllowWrites != (config.WriteMode{On: true, Origin: config.OriginConfigFile}) {
		t.Fatalf("AllowWrites = %+v, want on from the config file", cfg.AllowWrites)
	}
}

func TestLoadLetsTheEnvironmentWriteModeWinOverTheConfigFile(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{"LANGFUSE_MCP_ALLOW_WRITES": "false"},
		configFile("LANGFUSE_MCP_ALLOW_WRITES=true\n"))

	if cfg.AllowWrites != (config.WriteMode{On: false, Origin: config.OriginEnvironment}) {
		t.Fatalf("AllowWrites = %+v, want off from the environment", cfg.AllowWrites)
	}
}

// Dangerous parameters: any value but true or false stops startup, naming the
// variable and its source, never the value, which could be a misfiled secret.
func TestLoadRefusesAnInvalidWriteModeNamingTheVariableAndSourceButNotTheValue(t *testing.T) {
	t.Parallel()
	values := map[string]string{ //nolint:gosec // G101: a fake key, to prove the value is never quoted
		"yes":          "yes",
		"one":          "1",
		"upper case":   "TRUE",
		"a secret":     "sk-lf-typo-secret",
		"control char": "true\x00",
	}
	for name, value := range values {
		t.Run("environment/"+name, func(t *testing.T) {
			t.Parallel()

			_, err := load(map[string]string{"LANGFUSE_MCP_ALLOW_WRITES": value}, config.File{})

			if err == nil {
				t.Fatalf("Load accepted LANGFUSE_MCP_ALLOW_WRITES=%q", value)
			}
			msg := err.Error()
			if !strings.Contains(msg, "LANGFUSE_MCP_ALLOW_WRITES") || !strings.Contains(msg, "environment") {
				t.Errorf("error %q, want it to name LANGFUSE_MCP_ALLOW_WRITES and the environment", msg)
			}
			if strings.Contains(msg, value) {
				t.Errorf("error %q quotes the value", msg)
			}
		})
	}
	for name, value := range map[string]string{"yes": "yes", "a secret": "sk-lf-typo-secret"} { //nolint:gosec // G101: a fake key, to prove the value is never quoted
		t.Run("config file/"+name, func(t *testing.T) {
			t.Parallel()
			file := configFile("LANGFUSE_MCP_ALLOW_WRITES=" + value + "\n")

			_, err := load(map[string]string{}, file)

			if err == nil {
				t.Fatalf("Load accepted LANGFUSE_MCP_ALLOW_WRITES=%q in the config file", value)
			}
			msg := err.Error()
			if !strings.Contains(msg, "LANGFUSE_MCP_ALLOW_WRITES") || !strings.Contains(msg, file.Path) {
				t.Errorf("error %q, want it to name LANGFUSE_MCP_ALLOW_WRITES and the config file", msg)
			}
			if strings.Contains(msg, value) {
				t.Errorf("error %q quotes the value", msg)
			}
		})
	}
}
