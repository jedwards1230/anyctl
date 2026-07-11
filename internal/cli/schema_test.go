package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// TestSchemaCommand: `anyctl schema` emits the embedded JSON Schema as valid JSON
// containing the draft-07 $schema declaration.
func TestSchemaCommand(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	if code := Run([]string{"schema"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, `"$schema"`) {
		t.Fatalf("schema output missing $schema declaration: %q", got)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("schema output is not valid JSON: %v", err)
	}
	if s, _ := doc["$schema"].(string); !strings.Contains(s, "draft-07") {
		t.Fatalf("$schema = %q, want a draft-07 URI", s)
	}
}

// TestSchemaCommandArg: the optional positional arg selects the schema. `manifest`
// (explicit) matches the default, `catalog` emits the catalog-index schema, and
// an unknown kind is a usage error.
func TestSchemaCommandArg(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", t.TempDir())

	// Default (no arg) and explicit `manifest` are identical.
	var def, defErr bytes.Buffer
	if code := Run([]string{"schema"}, &def, &defErr); code != agentsafety.ExitOK {
		t.Fatalf("schema exit = %d (stderr: %s)", code, defErr.String())
	}
	var man, manErr bytes.Buffer
	if code := Run([]string{"schema", "manifest"}, &man, &manErr); code != agentsafety.ExitOK {
		t.Fatalf("schema manifest exit = %d (stderr: %s)", code, manErr.String())
	}
	if def.String() != man.String() {
		t.Error("`schema` and `schema manifest` should emit identical output")
	}
	if !strings.Contains(man.String(), "service manifest") {
		t.Errorf("schema manifest should describe a service manifest:\n%s", man.String())
	}

	// `catalog` emits the catalog-index schema (valid JSON, mentions the index).
	var cat, catErr bytes.Buffer
	if code := Run([]string{"schema", "catalog"}, &cat, &catErr); code != agentsafety.ExitOK {
		t.Fatalf("schema catalog exit = %d (stderr: %s)", code, catErr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(cat.Bytes(), &doc); err != nil {
		t.Fatalf("catalog schema output is not valid JSON: %v", err)
	}
	if !strings.Contains(cat.String(), "catalog index") {
		t.Errorf("schema catalog should describe the catalog index:\n%s", cat.String())
	}

	// An unknown kind is a usage error (exit 2).
	var out, errb bytes.Buffer
	if code := Run([]string{"schema", "bogus"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("schema bogus exit = %d, want %d (usage)", code, agentsafety.ExitUsage)
	}
}
