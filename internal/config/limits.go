package config

import (
	"fmt"
	"strconv"
)

// Names of the variables that bound the server's calls to Langfuse (#37).
const (
	// EnvRateLimit is the most Langfuse requests the server sends per minute.
	EnvRateLimit = "LANGFUSE_MCP_RATE_LIMIT"
	// EnvMaxConcurrency is the most Langfuse requests in flight at once.
	EnvMaxConcurrency = "LANGFUSE_MCP_MAX_CONCURRENCY"
)

// Ranges of the limits. The upper bounds only catch absurd values: 60,000 per
// minute is sixty times Langfuse Cloud's largest General API limit, and
// Langfuse Cloud answers any more concurrent calls with 429s anyway.
const (
	maxRateLimit      = 60000
	maxMaxConcurrency = 64
)

// loadLimit reads one limit setting: 0 when unset, else a whole number in
// 1..upper. A value out of range is refused, never clamped. Errors name the
// variable; a config file error never quotes the value, which could be a
// misfiled secret.
func loadLimit(s Setting, variable string, upper int, unit, filePath string) (int, error) {
	if s.Value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s.Value)
	if err == nil && n >= 1 && n <= upper {
		return n, nil
	}
	want := fmt.Sprintf("want a whole number of %s from 1 to %d", unit, upper)
	if s.Origin == OriginConfigFile {
		return 0, fmt.Errorf("config file %s: %s: %s", filePath, variable, want)
	}
	return 0, fmt.Errorf("%s=%q: %s", variable, s.Value, want)
}
