//go:build smoke

package main

import (
	"encoding/json"
	"os"
	"testing"
)

// Seam S2 (spec #119): the installed artifact. The smoke runs the assertions
// of checkInstalledArtifact against a release artifact launched by a command
// given from outside, so each channel is only a different launch command:
//
//	LANGFUSE_MCP_SMOKE_COMMAND  the launch command as a JSON array of strings
//	                            (an argv, never a shell string), e.g.
//	                            ["/tmp/x/langfuse-mcp"]
//	LANGFUSE_MCP_SMOKE_VERSION  the version the artifact must report
//	LANGFUSE_MCP_SMOKE_CA_DIR   optional: an existing directory the private CA
//	                            file is written to; the container adapter
//	                            mounts it read-only at the same path, so
//	                            LANGFUSE_CA_CERT names the file inside too
//
// Run: go test -tags smoke -count=1 -run '^TestInstalledArtifact$' ./cmd/langfuse-mcp/
// The release workflow (.github/workflows/release.yml) runs it on each runner.
const (
	smokeCommandEnv = "LANGFUSE_MCP_SMOKE_COMMAND"
	smokeVersionEnv = "LANGFUSE_MCP_SMOKE_VERSION"
	smokeCADirEnv   = "LANGFUSE_MCP_SMOKE_CA_DIR"
)

func TestInstalledArtifact(t *testing.T) {
	// A missing variable fails, never skips: a smoke that tested nothing must not pass.
	var argv []string
	if err := json.Unmarshal([]byte(os.Getenv(smokeCommandEnv)), &argv); err != nil || len(argv) == 0 || argv[0] == "" {
		t.Fatalf("%s must hold the launch command as a non-empty JSON array of strings: %v", smokeCommandEnv, err)
	}
	version := os.Getenv(smokeVersionEnv)
	if version == "" {
		t.Fatalf("%s must hold the version the artifact reports", smokeVersionEnv)
	}
	checkInstalledArtifact(t, argv, version, os.Getenv(smokeCADirEnv))
}
