package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

const (
	testPublicKey = "pk-lf-1111-public"
	testSecretKey = "sk-lf-2222-secret" //nolint:gosec // G101: a fake key, planted to prove it never prints
)

// connectionEnv returns a minimal valid connection environment plus extra.
func connectionEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"LANGFUSE_BASE_URL":   "https://cloud.langfuse.com",
		"LANGFUSE_PUBLIC_KEY": testPublicKey,
		"LANGFUSE_SECRET_KEY": testSecretKey,
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestLoadReadsTheHostFromLangfuseBaseURLOrItsAliasLangfuseHost(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env  map[string]string
		file string
		want string
	}{
		"LANGFUSE_BASE_URL": {
			env:  map[string]string{"LANGFUSE_BASE_URL": "https://us.cloud.langfuse.com"},
			want: "https://us.cloud.langfuse.com",
		},
		"LANGFUSE_HOST alias": {
			env:  map[string]string{"LANGFUSE_BASE_URL": "", "LANGFUSE_HOST": "https://jp.cloud.langfuse.com"},
			want: "https://jp.cloud.langfuse.com",
		},
		"LANGFUSE_BASE_URL wins over the alias": {
			env: map[string]string{
				"LANGFUSE_BASE_URL": "https://us.cloud.langfuse.com", "LANGFUSE_HOST": "https://jp.cloud.langfuse.com",
			},
			want: "https://us.cloud.langfuse.com",
		},
		"config file": {
			env:  map[string]string{"LANGFUSE_BASE_URL": ""},
			file: "LANGFUSE_BASE_URL=https://langfuse.internal.example.com\n",
			want: "https://langfuse.internal.example.com",
		},
		"the environment's alias wins over the config file": {
			env:  map[string]string{"LANGFUSE_BASE_URL": "", "LANGFUSE_HOST": "https://jp.cloud.langfuse.com"},
			file: "LANGFUSE_BASE_URL=https://langfuse.internal.example.com\n",
			want: "https://jp.cloud.langfuse.com",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Load(connectionEnv(tc.env), configFile(tc.file))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := cfg.Connection.Host.String(); got != tc.want {
				t.Fatalf("host = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLoadFailsNamingTheMissingConnectionVariable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		unset string
		want  string
	}{
		"host":       {unset: "LANGFUSE_BASE_URL", want: "LANGFUSE_BASE_URL"},
		"public key": {unset: "LANGFUSE_PUBLIC_KEY", want: "LANGFUSE_PUBLIC_KEY"},
		"secret key": {unset: "LANGFUSE_SECRET_KEY", want: "LANGFUSE_SECRET_KEY"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(connectionEnv(map[string]string{tc.unset: ""}), config.File{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

func TestLoadFailsNamingTheVariableOfAHostThatIsNotAnHTTPURL(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"cloud.langfuse.com", "ftp://cloud.langfuse.com", "https://"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			_, err := config.Load(connectionEnv(map[string]string{"LANGFUSE_BASE_URL": "", "LANGFUSE_HOST": host}), config.File{})
			if err == nil || !strings.Contains(err.Error(), "LANGFUSE_HOST") {
				t.Fatalf("Load() error = %v, want one naming LANGFUSE_HOST", err)
			}
		})
	}
}

func TestTheLangfuseKeysNeverPrintTheirValue(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(connectionEnv(nil), config.File{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Connection.PublicKey.Reveal() != testPublicKey || cfg.Connection.SecretKey.Reveal() != testSecretKey {
		t.Fatal("the key pair is not the one configured")
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "%v %+v %#v %s %v", cfg, cfg, cfg, cfg.Connection.SecretKey, cfg.Connection.PublicKey)
	slog.New(slog.NewJSONHandler(&out, nil)).Info("config", "config", cfg, "secret", cfg.Connection.SecretKey)
	slog.New(slog.NewTextHandler(&out, nil)).Info("config", "config", cfg, "secret", cfg.Connection.SecretKey)

	for _, key := range []string{testPublicKey, testSecretKey} {
		if strings.Contains(out.String(), key) {
			t.Fatalf("printed config leaks %s:\n%s", key, out.String())
		}
	}
}
