package schema_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/jedwards1230/anyctl/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const catalogSchemaURL = "https://raw.githubusercontent.com/jedwards1230/anyctl/main/schema/catalog.schema.json"

// compileCatalogSchema compiles the embedded catalog-index schema once per test,
// mirroring compileSchema for the manifest schema.
func compileCatalogSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Catalog))
	if err != nil {
		t.Fatalf("unmarshal embedded catalog schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(catalogSchemaURL, doc); err != nil {
		t.Fatalf("add catalog schema resource: %v", err)
	}
	sch, err := c.Compile(catalogSchemaURL)
	if err != nil {
		t.Fatalf("compile catalog schema: %v", err)
	}
	return sch
}

func catalogSchemaValidate(sch *jsonschema.Schema, src []byte) error {
	var v interface{}
	if err := yaml.Unmarshal(src, &v); err != nil {
		return err
	}
	return sch.Validate(v)
}

// TestReferenceCatalogIndexConformsToSchema: the shipped reference index
// (examples/catalog/anyctl-catalog.yaml) must validate clean against both the
// JSON Schema and the Go ParseCatalogIndex — the no-false-positives guarantee for
// the index a third-party author copies.
func TestReferenceCatalogIndexConformsToSchema(t *testing.T) {
	sch := compileCatalogSchema(t)
	path := filepath.Join("..", "examples", "catalog", manifest.CatalogIndexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := catalogSchemaValidate(sch, data); err != nil {
		t.Errorf("schema rejected the reference catalog index: %v", err)
	}
	if _, err := manifest.ParseCatalogIndex(data); err != nil {
		t.Errorf("ParseCatalogIndex rejected the reference catalog index: %v", err)
	}
}

// TestCatalogSchemaAndParseAgree exercises the rules both the JSON Schema and the
// Go ParseCatalogIndex enforce, asserting both engines reach the same verdict —
// catching drift between the hand-authored schema and the Go model.
func TestCatalogSchemaAndParseAgree(t *testing.T) {
	sch := compileCatalogSchema(t)
	cases := []struct {
		name      string
		yaml      string
		wantValid bool
	}{
		{"minimal (both accept)", "name: ref\ndescription: d\n", true},
		{"full (both accept)", "name: ref\ndescription: d\nversion: \"1.0\"\nhomepage: https://h\nmanifests: [a.yaml, b.yml]\n", true},
		{"missing name (both reject)", "description: d\n", false},
		{"missing description (both reject)", "name: ref\n", false},
		{"bad name (both reject)", "name: Ref\ndescription: d\n", false},
		{"unknown key (both reject)", "name: ref\ndescription: d\nbogus: 1\n", false},
		{"member path sep (both reject)", "name: ref\ndescription: d\nmanifests: [sub/a.yaml]\n", false},
		{"member non-yaml (both reject)", "name: ref\ndescription: d\nmanifests: [a.txt]\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schemaErr := catalogSchemaValidate(sch, []byte(tc.yaml))
			if (schemaErr == nil) != tc.wantValid {
				t.Errorf("schema valid=%v, want %v (err=%v)", schemaErr == nil, tc.wantValid, schemaErr)
			}
			_, parseErr := manifest.ParseCatalogIndex([]byte(tc.yaml))
			if (parseErr == nil) != tc.wantValid {
				t.Errorf("ParseCatalogIndex valid=%v, want %v (err=%v)", parseErr == nil, tc.wantValid, parseErr)
			}
		})
	}
}
