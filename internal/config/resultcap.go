package config

import "fmt"

// EnvMaxResultBytes sets the result size cap (CONTEXT.md): the most bytes of
// JSON one tool result returns to the agent.
const EnvMaxResultBytes = "LANGFUSE_MCP_MAX_RESULT_BYTES"

// Bounds and default of the result size cap. The default is below Claude Code's
// inline display limit, so a result at the cap is shown to an agent without file
// access; the upper bound is the host-independent ceiling of one result.
const (
	minMaxResultBytes     = 16 << 10
	maxMaxResultBytes     = 1 << 20
	defaultMaxResultBytes = 64 << 10
)

// ResultCap is the effective result size cap and where it was read.
type ResultCap struct {
	// Bytes is the cap; never 0 after Load.
	Bytes int
	// Origin is where the setting was read; "" when it is unset (the default).
	Origin Origin
}

// loadResultCap parses LANGFUSE_MCP_MAX_RESULT_BYTES: a plain decimal whole
// number from 16384 to 1048576; unset means 65536. Any other value, a unit, a
// sign or a number out of range included, is an error naming the variable and
// its source, never the value, which could be a misfiled secret. The value is
// never clamped.
func loadResultCap(s Setting, filePath string) (ResultCap, error) {
	if s.Value == "" {
		return ResultCap{Bytes: defaultMaxResultBytes}, nil
	}
	if n, ok := plainWholeNumber(s.Value); ok && n >= minMaxResultBytes && n <= maxMaxResultBytes {
		return ResultCap{Bytes: n, Origin: s.Origin}, nil
	}
	want := fmt.Sprintf("want a whole number of bytes from %d to %d", minMaxResultBytes, maxMaxResultBytes)
	if s.Origin == OriginConfigFile {
		return ResultCap{}, fmt.Errorf("config file %s: %s: %s", filePath, EnvMaxResultBytes, want)
	}
	return ResultCap{}, fmt.Errorf("%s (set in the environment): %s", EnvMaxResultBytes, want)
}

// plainWholeNumber parses s as ASCII digits only, so no sign, unit, separator
// or exponent is read; false when s is not that or does not fit an int.
func plainWholeNumber(s string) (int, bool) {
	n := 0
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
		if n > maxMaxResultBytes {
			return 0, false // out of range whatever follows; also stops an overflow
		}
	}
	return n, s != ""
}
