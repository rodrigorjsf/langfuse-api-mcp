package main

import "github.com/rodrigorjsf/langfuse-api-mcp/internal/config"

// envName is the key envMap stores a variable under. Windows variable names
// are case-insensitive, so each name is keyed as config.WindowsEnvName gives
// it, and a setting stored with any casing is found (#62).
func envName(name string) string { return config.WindowsEnvName(name) }
