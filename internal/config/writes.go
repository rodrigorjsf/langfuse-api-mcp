package config

import "fmt"

// EnvAllowWrites turns write mode on (ADR-0003): true registers execute_write,
// false or unset leaves the server read-only.
const EnvAllowWrites = "LANGFUSE_MCP_ALLOW_WRITES"

// WriteMode is whether write mode is on, and where that was read.
type WriteMode struct {
	// On is true only when LANGFUSE_MCP_ALLOW_WRITES is exactly true.
	On bool
	// Origin is where the setting was read; "" when it is unset (off).
	Origin Origin
}

// loadWriteMode parses LANGFUSE_MCP_ALLOW_WRITES: exactly true or false, unset
// means off. Any other value is an error naming the variable and its source,
// never the value: a typo must not silently leave writes on or off, and the
// value could be a misfiled secret.
func loadWriteMode(s Setting, filePath string) (WriteMode, error) {
	switch s.Value {
	case "":
		return WriteMode{}, nil
	case "true":
		return WriteMode{On: true, Origin: s.Origin}, nil
	case "false":
		return WriteMode{Origin: s.Origin}, nil
	}
	if s.Origin == OriginConfigFile {
		return WriteMode{}, fmt.Errorf("config file %s: %s: want true or false", filePath, EnvAllowWrites)
	}
	return WriteMode{}, fmt.Errorf("%s (set in the environment): want true or false", EnvAllowWrites)
}
