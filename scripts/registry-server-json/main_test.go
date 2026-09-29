package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The committed template: the one file the release renders for the MCP Registry.
func template(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "packaging", "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type rendered struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Packages    []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
		FileSha256   string `json:"fileSha256"`
		Environment  []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"environmentVariables"`
	} `json:"packages"`
}

func mustRender(t *testing.T, tmpl []byte, version string, bundle *mcpbBundle) rendered {
	t.Helper()
	out, err := render(tmpl, version, bundle)
	if err != nil {
		t.Fatalf("render(%q): %v", version, err)
	}
	var r rendered
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("rendered server.json is not JSON: %v\n%s", err, out)
	}
	return r
}

func TestReleaseServerJSONNamesTheNpmPackageAndTheImageAtTheReleaseVersion(t *testing.T) {
	r := mustRender(t, template(t), "0.1.0", nil)

	if r.Version != "0.1.0" {
		t.Errorf("version = %q, want 0.1.0", r.Version)
	}
	var got []string
	for _, p := range r.Packages {
		got = append(got, p.RegistryType+" "+p.Identifier+" "+p.Version)
	}
	want := []string{
		"npm langfuse-api-mcp 0.1.0",
		"oci ghcr.io/rodrigorjsf/langfuse-api-mcp:0.1.0 ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("packages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReleaseServerJSONListsTheBundleWithItsHashOnlyWhenTheReleaseHoldsIt(t *testing.T) {
	bundle := &mcpbBundle{Name: "langfuse-mcp_0.1.0.mcpb", Content: []byte("not really a bundle")}
	r := mustRender(t, template(t), "0.1.0", bundle)

	last := r.Packages[len(r.Packages)-1]
	if last.RegistryType != "mcpb" ||
		last.Identifier != "https://github.com/rodrigorjsf/langfuse-api-mcp/releases/download/v0.1.0/langfuse-mcp_0.1.0.mcpb" ||
		last.FileSha256 != "e8d034c0ae48e5d70f70c8e72fe60372fb0c5b938aba6cfd1ebacae7a9976462" {
		t.Errorf("mcpb package = %+v", last)
	}
	if len(r.Packages) != 3 {
		t.Errorf("got %d packages, want npm, oci and mcpb", len(r.Packages))
	}
}

func TestSnapshotVersionRendersAValidServerJSON(t *testing.T) {
	r := mustRender(t, template(t), "0.0.0-SNAPSHOT-0a1b2c3", nil)
	if r.Version != "0.0.0-SNAPSHOT-0a1b2c3" {
		t.Errorf("version = %q", r.Version)
	}
}

// Security gate (prompt injection): no Registry text is built from anything but the committed
// template. Only versions, identifiers and the bundle hash change; every text stays byte for byte.
func TestRegistryTextIsTheCommittedStaticText(t *testing.T) {
	var tmpl rendered
	if err := json.Unmarshal(template(t), &tmpl); err != nil {
		t.Fatal(err)
	}
	r := mustRender(t, template(t), "0.1.0", &mcpbBundle{Name: "langfuse-mcp_0.1.0.mcpb", Content: []byte("x")})

	if r.Name != "io.github.rodrigorjsf/langfuse-api-mcp" {
		t.Errorf("name = %q", r.Name)
	}
	if r.Title != tmpl.Title || r.Description != tmpl.Description {
		t.Errorf("title/description changed: %q / %q", r.Title, r.Description)
	}
	for i, p := range tmpl.Packages {
		for j, e := range p.Environment {
			if got := r.Packages[i].Environment[j]; got != e {
				t.Errorf("package %d variable %d = %+v, want %+v", i, j, got, e)
			}
		}
	}
}

// Dangerous parameters: the version and the bundle name come from the release workflow; anything
// that is not a release or snapshot version, or not a plain .mcpb file name, is refused.
func TestRefusesAVersionThatIsNotAReleaseOrSnapshotVersion(t *testing.T) {
	for _, v := range []string{"", "latest", "v0.1.0", "0.1", "0.1.0-rc.1", "0.1.0\n", "0.1.0;rm -rf /", "0.0.0-SNAPSHOT-XYZ", "0.1.0/../x"} {
		if _, err := render(template(t), v, nil); err == nil {
			t.Errorf("render(%q) succeeded, want an error", v)
		}
	}
}

func TestRefusesABundleNameThatIsNotAPlainMCPBFileName(t *testing.T) {
	for _, name := range []string{"", "bundle.zip", "../langfuse-mcp_0.1.0.mcpb", "a/b.mcpb", "a b.mcpb", "a?.mcpb", "a#.mcpb"} {
		if _, err := render(template(t), "0.1.0", &mcpbBundle{Name: name, Content: []byte("x")}); err == nil {
			t.Errorf("bundle name %q accepted, want an error", name)
		}
	}
}

// The render validates against the published schema it embeds, so a template the Registry
// would refuse fails the snapshot run, not the release.
func TestRefusesATemplateThePublishedSchemaRejects(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(template(t), &doc); err != nil {
		t.Fatal(err)
	}
	doc["description"] = strings.Repeat("x", 101) // the schema's maxLength is 100
	bad, _ := json.Marshal(doc)
	if _, err := render(bad, "0.1.0", nil); err == nil {
		t.Error("a 101-character description passed, want a schema error")
	}
}

// The embedded schema is the published file byte for byte: the SHA-256 of
// https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json on 2026-09-28.
func TestTheEmbeddedSchemaIsThePublishedFile(t *testing.T) {
	sum := sha256.Sum256(publishedSchema)
	if got := hex.EncodeToString(sum[:]); got != "3fba09590c99f61735d234822279f4223fab9e300c0a81e81c91ab62a4114de0" {
		t.Errorf("server.schema.json sha256 = %s: re-download it and update this pin together", got)
	}
}

func TestTheTemplateNamesTheEmbeddedPublishedSchema(t *testing.T) {
	var tmpl rendered
	if err := json.Unmarshal(template(t), &tmpl); err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(publishedSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if tmpl.Schema != schema.ID {
		t.Errorf("template $schema %q, embedded schema $id %q: re-vendor server.schema.json", tmpl.Schema, schema.ID)
	}
}
