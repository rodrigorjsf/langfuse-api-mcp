package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/config"
)

// Ticket #176: the result size cap is set by LANGFUSE_MCP_MAX_RESULT_BYTES.

func TestLoadDefaultsTheResultSizeCapTo64KiB(t *testing.T) {
	t.Parallel()

	cfg, err := load(map[string]string{}, config.File{})

	if want := (config.ResultCap{Bytes: 65536, Origin: ""}); err != nil || cfg.ResultCap != want {
		t.Fatalf("ResultCap = %+v, want %+v", cfg.ResultCap, want)
	}
}

func TestLoadReadsTheResultSizeCapFromTheEnvironmentTheConfigFileAndLetsTheEnvironmentWin(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		env  map[string]string
		file config.File
		want config.ResultCap
	}{
		"environment": {map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": "200000"}, config.File{},
			config.ResultCap{Bytes: 200000, Origin: config.OriginEnvironment}},
		"config file": {map[string]string{}, configFile("LANGFUSE_MCP_MAX_RESULT_BYTES=300000\n"),
			config.ResultCap{Bytes: 300000, Origin: config.OriginConfigFile}},
		"environment wins": {map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": "100000"},
			configFile("LANGFUSE_MCP_MAX_RESULT_BYTES=300000\n"),
			config.ResultCap{Bytes: 100000, Origin: config.OriginEnvironment}},
		"lower edge": {map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": "16384"}, config.File{},
			config.ResultCap{Bytes: 16384, Origin: config.OriginEnvironment}},
		"upper edge": {map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": "1048576"}, config.File{},
			config.ResultCap{Bytes: 1048576, Origin: config.OriginEnvironment}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := mustLoad(t, tc.env, tc.file)

			if cfg.ResultCap != tc.want {
				t.Fatalf("ResultCap = %+v, want %+v", cfg.ResultCap, tc.want)
			}
		})
	}
}

// Dangerous parameters: a value that is not a plain whole number in range
// stops startup naming the variable and its source, never the value; the cap
// is never clamped, so it cannot be raised above the upper bound.
func TestLoadRefusesAnInvalidResultSizeCapNamingTheVariableAndTheSourceButNotTheValue(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"not a number":      "sk-lf-6d5c4b3a-2f1e-4d0c-8b9a-7f6e5d4c3b2a",
		"looks like a pk":   "pk-lf-1a2b3c4d-5e6f-4a8b-9c0d-1e2f3a4b5c6d",
		"negative":          "-65536",
		"zero":              "0",
		"below the range":   "16383",
		"above the range":   "1048577",
		"absurdly large":    "99999999999999999999999",
		"with a unit":       "64KiB",
		"with a sign":       "+65536",
		"fraction":          "65536.5",
		"exponent":          "6.5e4",
		"hexadecimal":       "0x10000",
		"with blanks":       "65536 bytes",
		"with a control":    "65536\x01",
		"non-ASCII digits":  "٦٥٥٣٦",
		"with a separator":  "65_536",
		"leading blank run": " \t65536x",
	}
	for name, value := range values {
		t.Run("environment/"+name, func(t *testing.T) {
			t.Parallel()

			cfg, err := load(map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": value}, config.File{})

			if err == nil {
				t.Fatalf("Load(LANGFUSE_MCP_MAX_RESULT_BYTES=%q) = %+v, want a startup error", value, cfg.ResultCap)
			}
			msg := err.Error()
			if !strings.Contains(msg, "LANGFUSE_MCP_MAX_RESULT_BYTES") || !strings.Contains(msg, "environment") {
				t.Fatalf("error %q does not name the variable and the environment", msg)
			}
			if v := strings.TrimSpace(value); len(v) >= 5 && strings.Contains(msg, v) { // shorter values collide with the range in the message
				t.Fatalf("error %q quotes the value", msg)
			}
		})
		t.Run("config file/"+name, func(t *testing.T) {
			t.Parallel()
			file := configFile("LANGFUSE_MCP_MAX_RESULT_BYTES=" + value + "\n")

			_, err := load(map[string]string{}, file)

			if err == nil {
				t.Fatalf("Load(config file %q) succeeded, want a startup error", value)
			}
			msg := err.Error()
			if !strings.Contains(msg, "LANGFUSE_MCP_MAX_RESULT_BYTES") || !strings.Contains(msg, file.Path) {
				t.Fatalf("error %q does not name the variable and the config file", msg)
			}
			if v := strings.TrimSpace(value); len(v) >= 5 && strings.Contains(msg, v) {
				t.Fatalf("error %q quotes the value", msg)
			}
		})
	}
}

func TestLoadDoesNotWarnAboutTheResultSizeCapKeyInTheConfigFile(t *testing.T) {
	t.Parallel()

	_, ignored, err := config.Load(connectionEnv(nil), configFile("LANGFUSE_MCP_MAX_RESULT_BYTES=70000\n"))

	if err != nil || len(ignored) != 0 {
		t.Fatalf("Load = ignored %+v, err %v; want the key known: no warning, no error", ignored, err)
	}
}

func TestLoadStillOnlyWarnsAboutAnUnknownKeyNextToTheResultSizeCap(t *testing.T) {
	t.Parallel()

	_, ignored, err := config.Load(connectionEnv(nil),
		configFile("LANGFUSE_MCP_MAX_RESULT_BYTE=70000\nLANGFUSE_MCP_MAX_RESULT_BYTES=70000\n"))

	if err != nil || len(ignored) != 1 || ignored[0].Name != "LANGFUSE_MCP_MAX_RESULT_BYTE" {
		t.Fatalf("Load = ignored %+v, err %v; want only the misspelled key warned about", ignored, err)
	}
}

// FuzzLoadResultCap checks that Load never panics on a cap value and accepts
// exactly a plain decimal whole number from 16384 to 1048576, never quoting
// a refused one.
func FuzzLoadResultCap(f *testing.F) {
	for _, seed := range []string{"", "65536", "16384", "16383", "1048576", "1048577", "-1", "+65536", "64KiB", "1e5", "\x00", "00065536"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		cfg, err := load(map[string]string{"LANGFUSE_MCP_MAX_RESULT_BYTES": value}, config.File{})
		if value == "" {
			if err != nil || cfg.ResultCap.Bytes != 65536 {
				t.Fatalf("Load(\"\") = %+v, %v; want the default 65536", cfg.ResultCap, err)
			}
			return
		}
		n, parseErr := strconv.ParseUint(value, 10, 31) // ParseUint accepts no sign; "_" needs base 0
		valid := parseErr == nil && n >= 16384 && n <= 1048576
		if valid != (err == nil) {
			t.Fatalf("Load(%q) error = %v, want accepted: %v", value, err, valid)
		}
		if valid && cfg.ResultCap.Bytes != int(n) {
			t.Fatalf("Load(%q) = %d, want %d", value, cfg.ResultCap.Bytes, n)
		}
		if err != nil && len(value) >= 8 && strings.Contains(err.Error(), value) {
			t.Fatalf("error %q quotes the value", err)
		}
	})
}
