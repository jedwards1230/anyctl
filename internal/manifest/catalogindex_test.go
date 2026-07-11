package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestParseCatalogIndexValid: a well-formed index parses and every field is
// carried through, including a curated member list.
func TestParseCatalogIndexValid(t *testing.T) {
	const body = "name: reference\n" +
		"description: a two-service reference catalog\n" +
		"version: \"1.0.0\"\n" +
		"homepage: https://example.test/repo\n" +
		"manifests:\n" +
		"  - uptime.yaml\n" +
		"  - inventory.yml\n"
	idx, err := ParseCatalogIndex([]byte(body))
	if err != nil {
		t.Fatalf("ParseCatalogIndex: %v", err)
	}
	if idx.Name != "reference" || idx.Description != "a two-service reference catalog" {
		t.Errorf("name/description = %q/%q, want reference/...", idx.Name, idx.Description)
	}
	if idx.Version != "1.0.0" || idx.Homepage != "https://example.test/repo" {
		t.Errorf("version/homepage = %q/%q", idx.Version, idx.Homepage)
	}
	if len(idx.Manifests) != 2 || idx.Manifests[0] != "uptime.yaml" || idx.Manifests[1] != "inventory.yml" {
		t.Errorf("manifests = %v, want [uptime.yaml inventory.yml] in order", idx.Manifests)
	}
}

// TestParseCatalogIndexMinimal: name + description alone is valid (members
// auto-glob, version/homepage optional).
func TestParseCatalogIndexMinimal(t *testing.T) {
	idx, err := ParseCatalogIndex([]byte("name: mini\ndescription: minimal\n"))
	if err != nil {
		t.Fatalf("ParseCatalogIndex: %v", err)
	}
	if idx.Manifests != nil {
		t.Errorf("manifests = %v, want nil (auto-glob)", idx.Manifests)
	}
}

// TestParseCatalogIndexRejects table-drives every fail-closed rule: missing
// required fields, a bad name, an unknown key (strict decode), and malformed
// member entries (path separators, non-yaml). Every failure must be a
// *ConfigError (exit 2).
func TestParseCatalogIndexRejects(t *testing.T) {
	cases := map[string]string{
		"missing name":         "description: no name\n",
		"missing description":  "name: ref\n",
		"empty description":    "name: ref\ndescription: \"\"\n",
		"bad name uppercase":   "name: Ref\ndescription: d\n",
		"bad name dot":         "name: a.b\ndescription: d\n",
		"bad name slash":       "name: a/b\ndescription: d\n",
		"unknown key":          "name: ref\ndescription: d\nbogus: 1\n",
		"member path sep":      "name: ref\ndescription: d\nmanifests: [sub/uptime.yaml]\n",
		"member traversal":     "name: ref\ndescription: d\nmanifests: [../uptime.yaml]\n",
		"member non-yaml":      "name: ref\ndescription: d\nmanifests: [uptime.txt]\n",
		"member absolute path": "name: ref\ndescription: d\nmanifests: [/etc/uptime.yaml]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCatalogIndex([]byte(body))
			if err == nil {
				t.Fatalf("ParseCatalogIndex accepted invalid index %q", name)
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Errorf("want *ConfigError (exit 2), got %T: %v", err, err)
			}
		})
	}
}

// TestReadCatalogIndexMissing: a dir with no index yields ErrCatalogIndexMissing
// (detectable with errors.Is), so callers can print the starter hint.
func TestReadCatalogIndexMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCatalogIndex(dir)
	if !errors.Is(err, ErrCatalogIndexMissing) {
		t.Fatalf("ReadCatalogIndex(empty dir) = %v, want ErrCatalogIndexMissing", err)
	}
}

// TestReadCatalogIndexReadsFile: ReadCatalogIndex reads and parses the index at a
// dir's root.
func TestReadCatalogIndexReadsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, CatalogIndexFile), []byte("name: ref\ndescription: d\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := ReadCatalogIndex(dir)
	if err != nil {
		t.Fatalf("ReadCatalogIndex: %v", err)
	}
	if idx.Name != "ref" {
		t.Errorf("name = %q, want ref", idx.Name)
	}
}

// TestCatalogIndexFileName pins the well-known filename — it IS the discovery
// contract, so a change here is a change consumers must know about.
func TestCatalogIndexFileName(t *testing.T) {
	if CatalogIndexFile != "anyctl-catalog.yaml" {
		t.Errorf("CatalogIndexFile = %q, want anyctl-catalog.yaml", CatalogIndexFile)
	}
}
