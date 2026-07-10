package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/manifest"
)

// validManifestBody is a PORTABLE manifest (no base_url) — it passes plain
// `lint` and `list`, which only check structure. Binding lives in profile.yaml.
const validManifestBody = `
name: radarr
description: movie manager
auth:
  strategy: none
commands:
  list:
    method: GET
    path: /api/v3/movie
`

// TestLintValidService: `lint <name>` of a valid manifest prints "ok <name>" at
// exit 0.
func TestLintValidService(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "radarr", validManifestBody)
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	if code := Run([]string{"lint", "radarr"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(out.String(), "ok radarr") {
		t.Fatalf("stdout = %q, want 'ok radarr'", out.String())
	}
}

// TestLintSchemaBroken: a manifest with an unknown auth strategy fails the load
// with a ConfigError → exit 2 and a diagnostic on stderr.
func TestLintSchemaBroken(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "broken", `
name: broken
auth:
  strategy: not-a-real-strategy
commands:
  list:
    method: GET
    path: /x
`)
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	code := Run([]string{"lint", "broken"}, &out, &errb)
	if code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage/config)", code, agentsafety.ExitUsage)
	}
	if !strings.Contains(errb.String(), "strategy") {
		t.Fatalf("stderr = %q, want a diagnostic mentioning the bad strategy", errb.String())
	}
}

// TestLintFilePath: `lint <path.yaml>` validates the file directly.
func TestLintFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "standalone.yaml")
	if err := os.WriteFile(path, []byte(validManifestBody), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANYCTL_CONFIG_DIR", t.TempDir()) // empty config dir

	var out, errb bytes.Buffer
	if code := Run([]string{"lint", path}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if !strings.Contains(out.String(), "ok "+path) {
		t.Fatalf("stdout = %q, want 'ok %s'", out.String(), path)
	}
}

// TestLintStrictAggregates: `lint --strict` (no argument) reports EVERY service's
// completeness — not just the first alphabetical failure — so a user who has
// bound only one service still sees the bound one pass and the unbound ones fail,
// exiting 2. This is the fix for the old behavior that aborted on the first
// unbound service (e.g. "abs") even when the user only cared about "radarr".
func TestLintStrictAggregates(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "radarr", validManifestBody)
	// A second portable service that stays unbound.
	writeService(t, dir, "sonarr", `
name: sonarr
auth:
  strategy: none
commands:
  list: { method: GET, path: /api/v3/series }
`)
	// Bind only radarr's base_url in the profile; sonarr stays incomplete.
	bindBaseURL(t, dir, "radarr", "https://movies.example.com")
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	code := Run([]string{"lint", "--strict"}, &out, &errb)
	if code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage; some services incomplete)", code, agentsafety.ExitUsage)
	}
	// The bound service passes and the unbound one is reported — both appear, so
	// the check did not stop at the first failure.
	if !strings.Contains(out.String(), "ok radarr") {
		t.Errorf("stdout missing 'ok radarr':\n%s", out.String())
	}
	if !strings.Contains(out.String(), "FAIL sonarr") {
		t.Errorf("stdout missing 'FAIL sonarr' (should aggregate, not abort first):\n%s", out.String())
	}
}

// TestLintStrictScopedPasses: `lint --strict <service>` exits 0 when that one
// bound service is complete, even though other (embedded, unbound) services would
// fail an unscoped `lint --strict`. This is the "verify just the one I bound"
// path the `init` next-step hint promotes.
func TestLintStrictScopedPasses(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "radarr", validManifestBody)
	bindBaseURL(t, dir, "radarr", "https://movies.example.com")
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	if code := Run([]string{"lint", "--strict", "radarr"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (radarr is bound; stderr: %s)", code, errb.String())
	}
	if !strings.Contains(out.String(), "ok radarr") {
		t.Fatalf("stdout = %q, want 'ok radarr'", out.String())
	}
}

// TestLintUnknownService: `lint <unknown>` is a usage error (exit 2).
func TestLintUnknownService(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "radarr", validManifestBody)
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	if code := Run([]string{"lint", "nope"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, agentsafety.ExitUsage)
	}
}

// TestListDescriptions: `list` prints name + description columns.
func TestListDescriptions(t *testing.T) {
	dir := t.TempDir()
	writeService(t, dir, "radarr", validManifestBody)
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	var out, errb bytes.Buffer
	if code := Run([]string{"list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "radarr") || !strings.Contains(got, "movie manager") {
		t.Fatalf("list output = %q, want name + description", got)
	}
}

// TestProbeSkip covers every skip case of the pure skip classifier.
func TestProbeSkip(t *testing.T) {
	cases := []struct {
		name string
		svc  *manifest.Service
		skip bool
	}{
		{"empty base", &manifest.Service{}, true},
		{"templated base", &manifest.Service{BaseURL: "https://{host}:8080"}, true},
		{"wss base", &manifest.Service{BaseURL: "wss://x/ws"}, true},
		{"jsonrpc-ws transport", &manifest.Service{BaseURL: "http://x", Transport: "jsonrpc-ws"}, true},
		{"plain http", &manifest.Service{BaseURL: "http://x"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, skip := probeSkip(c.svc)
			if skip != c.skip {
				t.Fatalf("probeSkip skip = %v, want %v (reason %q)", skip, c.skip, reason)
			}
			if skip && reason == "" {
				t.Fatal("a skipped service must carry a reason")
			}
		})
	}
}

// TestProbeReachableUnreachable exercises the live probe against an httptest
// server (reachable) and a closed port (unreachable) — no real network.
func TestProbeReachableUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	if got := probe(&manifest.Service{BaseURL: srv.URL}); !strings.Contains(got, "reachable (HTTP 204)") {
		t.Fatalf("reachable probe = %q, want 'reachable (HTTP 204)'", got)
	}
	if got := probe(&manifest.Service{BaseURL: "http://127.0.0.1:1"}); !strings.HasPrefix(got, "unreachable:") {
		t.Fatalf("unreachable probe = %q, want 'unreachable: ...'", got)
	}
}
