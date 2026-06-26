package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jedwards1230/labctl/internal/command"
	"github.com/jedwards1230/labctl/internal/manifest"
)

// TestCursorNoProgressGuard proves a server that repeats the same cursor stops
// the pagination loop early instead of spinning to maxPages.
func TestCursorNoProgressGuard(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Always advertise the same next cursor "C" → no progress.
		_, _ = w.Write([]byte(`{"data":[{"id":1}],"next":"C"}`))
	}))
	defer srv.Close()

	svc := &manifest.Service{
		Name:    "paged",
		BaseURL: srv.URL,
		Auth:    manifest.Auth{Strategy: "none"},
		Pagination: manifest.Pagination{
			Style: "cursor",
			Param: "cursor",
			Data:  ".data",
			Next:  ".next",
		},
		Commands: map[string]manifest.Command{
			"list": {Method: "GET", Path: "/api/list"},
		},
	}
	cmds := command.FromManifest(svc)
	if _, err := Execute(t.Context(), Request{
		Config:  manifest.Config{},
		Service: svc,
		Command: cmds["list"],
		Runner:  fakeOp,
		Getenv:  func(string) string { return "" },
	}, nil); err != nil {
		t.Fatalf("execute cursor: %v", err)
	}
	// Page 1 (cursor=""), page 2 (cursor="C") returns next="C" == current → stop.
	if calls >= maxPages {
		t.Fatalf("cursor loop did not terminate early: %d calls", calls)
	}
	if calls != 2 {
		t.Fatalf("want 2 calls (stop on repeated cursor), got %d", calls)
	}
}

// TestAccVarToStringEncodesNonScalar proves a pipeline var holding a map/slice
// renders as JSON for {var} substitution, not Go's map[...] syntax; scalars
// stringify naturally.
func TestAccVarToStringEncodesNonScalar(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hi", "hi"},
		{"nil", nil, ""},
		{"bool", true, "true"},
		{"float", 3.5, "3.5"},
		{"object", map[string]any{"a": 1.0}, `{"a":1}`},
		{"array", []any{1.0, 2.0}, `[1,2]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := accVarToString(c.in)
			if got != c.want {
				t.Fatalf("accVarToString(%v) = %q, want %q", c.in, got, c.want)
			}
			if strings.Contains(got, "map[") {
				t.Fatalf("accVarToString leaked Go syntax: %q", got)
			}
		})
	}
}
