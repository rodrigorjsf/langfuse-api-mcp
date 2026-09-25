package config_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

func TestLoadReadsExplicitCASourcePathsFromTheEnvironment(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{
		"LANGFUSE_CA_CERT":       "/etc/corp/root.pem",
		"LANGFUSE_CA_CERTS_PATH": "/etc/corp/certs",
	}, config.File{})

	want := config.Config{
		CACert:      fromEnv("/etc/corp/root.pem"),
		CACertsPath: fromEnv("/etc/corp/certs"),
	}
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
			if cfg := mustLoad(t, env, config.File{}); cfg != (config.Config{}) {
				t.Fatalf("Load() = %+v, want no CA sources", cfg)
			}
		})
	}
}

// mustLoad calls config.Load and fails the test on an error.
func mustLoad(t *testing.T, env map[string]string, file config.File) config.Config {
	t.Helper()
	cfg, err := config.Load(env, file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func fromEnv(value string) config.Setting {
	return config.Setting{Value: value, Origin: config.OriginEnvironment}
}

func fromFile(value string) config.Setting {
	return config.Setting{Value: value, Origin: config.OriginConfigFile}
}

// configFile returns a config file with the given content at a fixed path.
func configFile(content string) config.File {
	return config.File{Path: "/home/u/.config/langfuse-mcp/config.env", Content: []byte(content)}
}

func TestLoadUsesAValueSetOnlyInTheConfigFile(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t, map[string]string{}, configFile("LANGFUSE_CA_CERT=/etc/corp/root.pem\n"))

	if want := fromFile("/etc/corp/root.pem"); cfg.CACert != want {
		t.Fatalf("CACert = %+v, want %+v", cfg.CACert, want)
	}
}

func TestLoadLetsTheEnvironmentOverrideTheConfigFile(t *testing.T) {
	t.Parallel()

	cfg := mustLoad(t,
		map[string]string{"LANGFUSE_CA_CERT": "/env/root.pem"},
		configFile("LANGFUSE_CA_CERT=/file/root.pem\nLANGFUSE_CA_CERTS_PATH=/file/certs\n"))

	want := config.Config{CACert: fromEnv("/env/root.pem"), CACertsPath: fromFile("/file/certs")}
	if cfg != want {
		t.Fatalf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadFailsNamingTheConfigFileAndLineNumberOfAMalformedLine(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"no equals sign": "LANGFUSE_CA_CERT=/etc/corp/root.pem\nthis is not a setting\n",
		"empty name":     "LANGFUSE_CA_CERT=/etc/corp/root.pem\n=/etc/corp/other.pem\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := configFile(content)

			_, err := config.Load(map[string]string{}, file)

			if err == nil {
				t.Fatal("Load() succeeded on a malformed config file")
			}
			if msg := err.Error(); !strings.Contains(msg, file.Path) || !strings.Contains(msg, "line 2") {
				t.Fatalf("error %q does not name %s and line 2", msg, file.Path)
			}
		})
	}
}

func TestLoadIgnoresCommentsAndBlankLinesInTheConfigFile(t *testing.T) {
	t.Parallel()
	content := "# corporate CA, set once for every client\n" +
		"\n" +
		"   \n" +
		"  # LANGFUSE_CA_CERTS_PATH=/commented/out\n" +
		"LANGFUSE_CA_CERT=/etc/corp/root.pem\n"

	cfg := mustLoad(t, map[string]string{}, configFile(content))

	want := config.Config{CACert: fromFile("/etc/corp/root.pem")}
	if cfg != want {
		t.Fatalf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadRefusesLangfuseKeysInTheConfigFile(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ line, variable, value string }{
		"public key":           {"LANGFUSE_PUBLIC_KEY=pk-lf-1234", "LANGFUSE_PUBLIC_KEY", "pk-lf-1234"},
		"secret key":           {"LANGFUSE_SECRET_KEY=sk-lf-5678", "LANGFUSE_SECRET_KEY", "sk-lf-5678"},
		"dotenv export prefix": {"export LANGFUSE_SECRET_KEY=sk-lf-5678", "LANGFUSE_SECRET_KEY", "sk-lf-5678"},
		"lower case":           {"langfuse_secret_key=sk-lf-5678", "LANGFUSE_SECRET_KEY", "sk-lf-5678"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(map[string]string{}, configFile(tc.line+"\n"))

			if err == nil {
				t.Fatalf("Load() accepted %q in the config file", tc.line)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.variable) || !strings.Contains(msg, "environment") {
				t.Fatalf("error %q does not say %s must come from the environment", msg, tc.variable)
			}
			if strings.Contains(msg, tc.value) {
				t.Fatalf("error %q leaks the key value", msg)
			}
		})
	}
}

func TestLoadReadsAConfigFileSavedByAWindowsEditor(t *testing.T) {
	t.Parallel()
	// Notepad may start the file with a UTF-8 byte order mark and end lines with CRLF.
	content := "\uFEFFLANGFUSE_CA_CERT=C:\\corp\\root.pem\r\nLANGFUSE_CA_CERTS_PATH=C:\\corp\\certs\r\n"

	cfg := mustLoad(t, map[string]string{}, configFile(content))

	want := config.Config{CACert: fromFile(`C:\corp\root.pem`), CACertsPath: fromFile(`C:\corp\certs`)}
	if cfg != want {
		t.Fatalf("Load() = %+v, want %+v", cfg, want)
	}
}

// FuzzLoadConfigFile checks that any config file content either loads or fails
// with one of the two fixed error shapes, which name the file, the line number
// and at most a key variable, so the content (possibly a secret) is never quoted.
func FuzzLoadConfigFile(f *testing.F) {
	f.Add("LANGFUSE_CA_CERT=/etc/corp/root.pem\n# comment\n\n")
	f.Add("\uFEFFLANGFUSE_CA_CERTS_PATH=C:\\corp\\certs\r\n")
	f.Add("export LANGFUSE_SECRET_KEY=sk-lf-secret\n")
	f.Add("=value\nno equals sign")
	path := regexp.QuoteMeta(configFile("").Path)
	shape := regexp.MustCompile(`^config file ` + path + ` line [0-9]+: (expected KEY=VALUE|` +
		`LANGFUSE_(PUBLIC|SECRET)_KEY is not allowed in the config file; ` +
		`set it in the environment or in your MCP client's env block)$`)
	f.Fuzz(func(t *testing.T, content string) {
		_, err := config.Load(map[string]string{}, configFile(content))
		if err != nil && !shape.MatchString(err.Error()) {
			t.Fatalf("error %q is not one of the fixed shapes", err)
		}
	})
}
