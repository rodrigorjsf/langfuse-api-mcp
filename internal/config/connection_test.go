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
			cfg, _, err := config.Load(connectionEnv(tc.env), configFile(tc.file))
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
			_, _, err := config.Load(connectionEnv(map[string]string{tc.unset: ""}), config.File{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

func TestLoadFailsNamingTheVariableOfAKeyWithoutItsLangfusePrefix(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env      map[string]string
		want     string
		wantHint string
	}{
		"public key without pk-lf-": {
			env: map[string]string{"LANGFUSE_PUBLIC_KEY": "1111-public"}, want: "LANGFUSE_PUBLIC_KEY", wantHint: "pk-lf-",
		},
		"secret key without sk-lf-": {
			env: map[string]string{"LANGFUSE_SECRET_KEY": "2222-secret"}, want: "LANGFUSE_SECRET_KEY", wantHint: "sk-lf-",
		},
		"swapped pair": {
			env:  map[string]string{"LANGFUSE_PUBLIC_KEY": testSecretKey, "LANGFUSE_SECRET_KEY": testPublicKey},
			want: "LANGFUSE_PUBLIC_KEY", wantHint: "swapped",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := config.Load(connectionEnv(tc.env), config.File{})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.wantHint) {
				t.Fatalf("Load() error = %v, want one naming %s and saying %q", err, tc.want, tc.wantHint)
			}
			for _, v := range tc.env {
				if strings.Contains(err.Error(), v) {
					t.Errorf("Load() error %q echoes the key value", err)
				}
			}
		})
	}
}

func TestLoadFailsNamingTheVariableOfAHostThatIsNotAnHTTPURL(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"cloud.langfuse.com", "ftp://cloud.langfuse.com", "https://"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			_, _, err := config.Load(connectionEnv(map[string]string{"LANGFUSE_BASE_URL": "", "LANGFUSE_HOST": host}), config.File{})
			if err == nil || !strings.Contains(err.Error(), "LANGFUSE_HOST") {
				t.Fatalf("Load() error = %v, want one naming LANGFUSE_HOST", err)
			}
		})
	}
}

func TestLoadRequiresHTTPSExceptForALoopbackHost(t *testing.T) {
	t.Parallel()
	for host, wantOK := range map[string]bool{
		"https://langfuse.internal.example.com": true,
		"http://localhost:3000":                 true,
		"http://LOCALHOST:3000":                 true,
		"http://127.0.0.1:3000":                 true,
		"http://127.1.2.3":                      true,
		"http://[::1]:3000":                     true,
		"http://langfuse.internal.example.com":  false,
		"http://10.0.0.5:3000":                  false,
		"http://localhost.evil.example":         false,
		"http://127.0.0.1.evil.example":         false,
		"http://[::ffff:10.0.0.5]:3000":         false,
	} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			_, _, err := config.Load(connectionEnv(map[string]string{"LANGFUSE_BASE_URL": host}), config.File{})
			switch {
			case wantOK && err != nil:
				t.Fatalf("Load() error = %v, want %s accepted", err, host)
			case !wantOK && (err == nil || !strings.Contains(err.Error(), "LANGFUSE_BASE_URL") || !strings.Contains(err.Error(), "https")):
				t.Fatalf("Load() error = %v, want one naming LANGFUSE_BASE_URL and requiring https", err)
			}
		})
	}
}

func TestTheLangfuseKeysNeverPrintTheirValue(t *testing.T) {
	t.Parallel()
	cfg, _, err := config.Load(connectionEnv(nil), config.File{})
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

func TestASecretRendersRedactedForEveryFmtVerb(t *testing.T) {
	t.Parallel()
	cfg, _, err := config.Load(connectionEnv(nil), config.File{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	secret := cfg.Connection.SecretKey
	// String covers only %v %s %x %X %q: other verbs print the struct field by
	// reflection unless Secret formats itself (#46).
	for _, verb := range []string{"%d", "%t", "%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%10.3f"} {
		format := verb // a variable format string: vet would reject the wrong-type verbs
		if got := fmt.Sprintf(format, secret); got != "[REDACTED]" {
			t.Errorf("Sprintf(%q, secret) = %q, want [REDACTED]", verb, got)
		}
	}
}
