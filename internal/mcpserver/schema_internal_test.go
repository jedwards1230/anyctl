package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/jedwards1230/anyctl/internal/command"
	"github.com/jedwards1230/anyctl/internal/manifest"
)

// schemaProps unmarshals a buildSchema result and returns its properties map.
func schemaProps(t *testing.T, c *command.Command) map[string]any {
	t.Helper()
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(buildSchema(c), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	return schema.Properties
}

// TestMaxArgIndexParams proves a ws `call` command with args in Params surfaces
// arg0/arg1 in the input schema.
func TestMaxArgIndexParams(t *testing.T) {
	c := &command.Command{
		Method: "pool.query",
		Params: `["{arg.0}", "{arg1}"]`,
	}
	if got := maxArgIndex(c); got != 1 {
		t.Fatalf("maxArgIndex = %d, want 1 (params args)", got)
	}
	props := schemaProps(t, c)
	if _, ok := props["arg0"]; !ok {
		t.Error("schema missing arg0 for a params arg")
	}
	if _, ok := props["arg1"]; !ok {
		t.Error("schema missing arg1 for a params arg")
	}
}

// TestMaxArgIndexSteps proves a pipeline command with an arg in a step's path
// surfaces that arg in the input schema.
func TestMaxArgIndexSteps(t *testing.T) {
	c := &command.Command{
		Steps: []manifest.Step{
			{ID: "a", Method: "GET", Path: "/lookup/{arg.0}"},
			{ID: "b", Method: "POST", Path: "/act", Body: `{"q":"{arg.1}"}`},
		},
	}
	if got := maxArgIndex(c); got != 1 {
		t.Fatalf("maxArgIndex = %d, want 1 (step args)", got)
	}
	props := schemaProps(t, c)
	if _, ok := props["arg0"]; !ok {
		t.Error("schema missing arg0 for a step path arg")
	}
	if _, ok := props["arg1"]; !ok {
		t.Error("schema missing arg1 for a step body arg")
	}
}

// TestMaxArgIndexIgnoresJQ proves jq fields (extract/when/body_transform) are
// NOT scanned for template args — those are jq, not templates.
func TestMaxArgIndexIgnoresJQ(t *testing.T) {
	c := &command.Command{
		Steps: []manifest.Step{
			{
				ID:            "a",
				Method:        "GET",
				Path:          "/x",
				Extract:       map[string]string{"v": ".items[0]"},
				When:          ".ok == true",
				BodyTransform: "{q: .id}",
			},
		},
	}
	if got := maxArgIndex(c); got != -1 {
		t.Fatalf("maxArgIndex = %d, want -1 (jq fields must not count as args)", got)
	}
}
