package config_test

import (
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

func TestLoadReadsExplicitCASourcePathsFromTheEnvironment(t *testing.T) {
	t.Parallel()

	cfg := config.Load(map[string]string{
		"LANGFUSE_CA_CERT":       "/etc/corp/root.pem",
		"LANGFUSE_CA_CERTS_PATH": "/etc/corp/certs",
	})

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
			if cfg := config.Load(env); cfg != (config.Config{}) {
				t.Fatalf("Load() = %+v, want no CA sources", cfg)
			}
		})
	}
}
