// Command registry-server-json renders the MCP Registry entry of a build (#154) from the committed
// template packaging/server.json and validates it against the Registry's published schema.
//
// WHAT: reads the template, sets the build's version on the entry, the npm package and the OCI
// image tag, adds the MCPB bundle with its fileSha256 when one is given, validates the result
// against server.schema.json (embedded below) and writes it to stdout. Nothing else changes: every
// text (name, title, description, variable descriptions) is the template's, never built from API
// data (security gate, prompt injection). The version must be a release (X.Y.Z) or snapshot
// (0.0.0-SNAPSHOT-<hex>) version and the bundle a plain *.mcpb file name, or it stops.
//
// WHY: the Registry checks server.json only when a release publishes it, and that job may fail
// without failing the release (the Registry is in preview). Rendering and validating the same
// file offline in every snapshot run catches a broken entry before a tag, without calling the
// Registry, so its availability can never block a release.
//
// WHEN: in .github/workflows/release.yml: the snapshot job renders and validates the snapshot's
// entry (with its bundle); on a `v*` tag publish-registry renders the release's entry, with the
// bundle only if the GitHub release holds one, and publishes it with mcp-publisher.
//
// HOW:
//
//	go run ./scripts/registry-server-json -version 0.1.0 [-mcpb dist/langfuse-mcp_0.1.0.mcpb] packaging/server.json > server.json
//
// server.schema.json is https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json
// byte for byte (sha256 3fba09590c99f61735d234822279f4223fab9e300c0a81e81c91ab62a4114de0), the
// schema mcp-publisher v1.8.1 embeds. When the template's $schema moves to a newer schema,
// re-download it here; a test fails while the two differ.
package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed server.schema.json
var publishedSchema []byte

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$|^0\.0\.0-SNAPSHOT-[0-9a-f]+$`)
	bundlePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+\.mcpb$`)
)

// mcpbBundle is the MCPB file of the release: its file name on the GitHub release and its bytes.
type mcpbBundle struct {
	Name    string
	Content []byte
}

func main() {
	version := flag.String("version", "", "the build's version: X.Y.Z or 0.0.0-SNAPSHOT-<hex>")
	mcpb := flag.String("mcpb", "", "the release's .mcpb file, if the release holds one")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: registry-server-json -version <version> [-mcpb <file>] <template>")
		os.Exit(2)
	}
	tmpl, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	var bundle *mcpbBundle
	if *mcpb != "" {
		content, err := os.ReadFile(*mcpb)
		if err != nil {
			fail(err)
		}
		bundle = &mcpbBundle{Name: filepath.Base(*mcpb), Content: content}
	}
	out, err := render(tmpl, *version, bundle)
	if err != nil {
		fail(err)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "registry-server-json:", err)
	os.Exit(1)
}

// render returns the Registry entry for version: the template with the version set on the entry,
// the npm package and the OCI image tag, plus the MCPB package when bundle is not nil. The result
// is validated against the published schema.
func render(tmpl []byte, version string, bundle *mcpbBundle) ([]byte, error) {
	if !versionPattern.MatchString(version) {
		return nil, errors.New("-version must be X.Y.Z or 0.0.0-SNAPSHOT-<hex>")
	}
	var doc map[string]any
	if err := json.Unmarshal(tmpl, &doc); err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	doc["version"] = version
	packages, _ := doc["packages"].([]any)
	for _, p := range packages {
		pkg, ok := p.(map[string]any)
		if !ok {
			return nil, errors.New("template: a package is not an object")
		}
		switch pkg["registryType"] {
		case "npm":
			pkg["version"] = version
		case "oci":
			id, _ := pkg["identifier"].(string)
			repo, _, found := strings.Cut(id, ":")
			if !found || repo == "" {
				return nil, errors.New("template: the oci identifier has no tag")
			}
			pkg["identifier"] = repo + ":" + version
		default:
			return nil, fmt.Errorf("template: unexpected package type %v", pkg["registryType"])
		}
	}
	if bundle != nil {
		if !bundlePattern.MatchString(bundle.Name) {
			return nil, errors.New("-mcpb must name a plain *.mcpb file")
		}
		repo, _ := doc["repository"].(map[string]any)
		url, _ := repo["url"].(string)
		if !strings.HasPrefix(url, "https://github.com/") {
			return nil, errors.New("template: repository.url is not a GitHub repository")
		}
		sum := sha256.Sum256(bundle.Content)
		packages = append(packages, map[string]any{
			"registryType": "mcpb",
			"identifier":   url + "/releases/download/v" + version + "/" + bundle.Name,
			"fileSha256":   hex.EncodeToString(sum[:]),
			"transport":    map[string]any{"type": "stdio"},
		})
		doc["packages"] = packages
	}
	if err := validate(doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func validate(doc map[string]any) error {
	var schema jsonschema.Schema
	if err := json.Unmarshal(publishedSchema, &schema); err != nil {
		return fmt.Errorf("embedded schema: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("embedded schema: %w", err)
	}
	if err := resolved.Validate(doc); err != nil {
		return fmt.Errorf("server.json does not match the published schema: %w", err)
	}
	return nil
}
