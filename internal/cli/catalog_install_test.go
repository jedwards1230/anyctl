package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// writeSourceManifest writes a manifest into a source dir used by `catalog add`.
func writeSourceManifest(t *testing.T, srcDir, fname, body string) {
	t.Helper()
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, fname), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeCatalogIndex writes the required anyctl-catalog.yaml index into a catalog
// source dir. name is the index's catalog name (the default install name);
// members, when non-empty, pins an explicit ordered `manifests:` list, otherwise
// the catalog auto-globs its top-level manifests.
func writeCatalogIndex(t *testing.T, srcDir, name string, members ...string) {
	t.Helper()
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: " + name + "\ndescription: test catalog " + name + "\n"
	if len(members) > 0 {
		body += "manifests:\n"
		for _, m := range members {
			body += "  - " + m + "\n"
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "anyctl-catalog.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const portableWidget = `name: widget
description: a widget
auth: { strategy: none }
commands:
  list: { method: GET, path: /list }
`

// TestCatalogAddDirSource: adding a local dir source installs the catalog under
// its index `name` and its services load with origin catalog:<name>; `catalog
// list` reports it.
func TestCatalogAddDirSource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "srcdir")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeCatalogIndex(t, src, "mycat")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	// Installed under the index name (not the dir basename — inference is gone).
	manifestPath := filepath.Join(cfg, "catalogs", "mycat", "widget.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("catalog manifest not installed: %v", err)
	}
	// The index file is NOT copied into the installed catalog dir.
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "mycat", "anyctl-catalog.yaml")); !os.IsNotExist(err) {
		t.Error("the index file must not be copied into the installed catalog dir")
	}

	// list shows the service with its catalog provenance.
	out.Reset()
	errb.Reset()
	if code := Run([]string{"list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("list exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("widget")) || !bytes.Contains(out.Bytes(), []byte("catalog:mycat")) {
		t.Errorf("list output should mark widget as catalog:mycat:\n%s", out.String())
	}

	// `catalog list` reports it (data to stdout).
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("catalog list exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("mycat")) {
		t.Errorf("`catalog list` should list mycat:\n%s", out.String())
	}
}

// TestCatalogAddRejectsNonSchemaManifest: a manifest that doesn't conform to the
// schema rejects the whole add; nothing is installed.
func TestCatalogAddRejectsNonSchemaManifest(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "badcat")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeSourceManifest(t, src, "bad.yaml", "name: bad\nbogus_key: 1\nauth: { strategy: none }\n")
	writeCatalogIndex(t, src, "badcat")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code == agentsafety.ExitOK {
		t.Fatalf("add of a non-schema manifest should fail (stderr: %s)", errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "badcat")); !os.IsNotExist(err) {
		t.Error("nothing should be installed when one manifest is invalid")
	}
}

// TestCatalogAddRejectsBindingManifest: a manifest carrying a base_url or secret
// ref is non-portable — the add is rejected and nothing is written.
func TestCatalogAddRejectsBindingManifest(t *testing.T) {
	for name, body := range map[string]string{
		"base_url":   "name: bound\nbase_url: https://h.example\nauth: { strategy: none }\n",
		"secret-ref": "name: bound\nsecrets:\n  token: { ref: op://v/i/f }\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := t.TempDir()
			t.Setenv("ANYCTL_CONFIG_DIR", cfg)
			src := filepath.Join(t.TempDir(), "boundcat")
			writeSourceManifest(t, src, "bound.yaml", body)
			writeCatalogIndex(t, src, "boundcat")

			var out, errb bytes.Buffer
			if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitUsage {
				t.Fatalf("add exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
			}
			if _, err := os.Stat(filepath.Join(cfg, "catalogs", "boundcat")); !os.IsNotExist(err) {
				t.Error("a binding-carrying manifest must not be installed")
			}
		})
	}
}

// TestCatalogAddNoManifests: a source that carries an index but no member
// manifests is a usage error ('no manifests').
func TestCatalogAddNoManifests(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := t.TempDir() // only an index, no manifests
	writeCatalogIndex(t, src, "empty")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("no manifests")) {
		t.Errorf("stderr = %q, want a 'no manifests' diagnostic", errb.String())
	}
}

// TestCatalogAddMissingIndex: a source dir with manifests but no
// anyctl-catalog.yaml index is a usage error that names the index file (basename
// inference is gone; the index is the discovery contract). Nothing is installed.
func TestCatalogAddMissingIndex(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "noidx")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src, "--name", "noidx"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("anyctl-catalog.yaml")) {
		t.Errorf("stderr = %q, want it to name the missing index file", errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "noidx")); !os.IsNotExist(err) {
		t.Error("nothing should be installed when the index is missing")
	}
}

// TestCatalogAddCrossCatalogCollision: adding a second catalog that defines a
// service already provided by an installed catalog now SUCCEEDS — both catalogs
// install, each addressable via its qualified "<catalog>:<service>" selector
// (`anyctl svc <catalog>:<service>`), while the bare name is ambiguous and
// reported by `list`/`anyctl svc <name>` rather than silently picked.
func TestCatalogAddCrossCatalogCollision(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	srcA := filepath.Join(t.TempDir(), "acat")
	writeSourceManifest(t, srcA, "widget.yaml", portableWidget)
	writeCatalogIndex(t, srcA, "acat")
	srcB := filepath.Join(t.TempDir(), "bcat")
	writeSourceManifest(t, srcB, "widget.yaml", portableWidget)
	writeCatalogIndex(t, srcB, "bcat")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", srcA}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add acat exit = %d (stderr: %s)", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "add", srcB}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add bcat exit = %d, want %d (stderr: %s)", code, agentsafety.ExitOK, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "bcat")); err != nil {
		t.Errorf("the second catalog should now install alongside the first: %v", err)
	}

	// `list` shows both qualified forms, never the bare ambiguous name.
	out.Reset()
	errb.Reset()
	if code := Run([]string{"list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("list exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("acat:widget")) || !bytes.Contains(out.Bytes(), []byte("bcat:widget")) {
		t.Errorf("list output should show both qualified forms:\n%s", out.String())
	}

	// The bare name is ambiguous: both `anyctl svc widget` (no subcommand) and
	// `anyctl svc widget list` (with one) must error with the qualify message.
	for _, args := range [][]string{{"svc", "widget"}, {"svc", "widget", "list"}} {
		out.Reset()
		errb.Reset()
		if code := Run(args, &out, &errb); code != agentsafety.ExitUsage {
			t.Errorf("Run(%v) exit = %d, want %d (usage) (stderr: %s)", args, code, agentsafety.ExitUsage, errb.String())
		}
		if !bytes.Contains(errb.Bytes(), []byte("acat:widget")) || !bytes.Contains(errb.Bytes(), []byte("bcat:widget")) {
			t.Errorf("Run(%v) stderr = %q, want it to list both qualified forms", args, errb.String())
		}
	}

	// The qualified form dispatches normally (profile binding is by the
	// underlying manifest's service name, so it applies to either catalog's copy).
	bindBaseURL(t, cfg, "widget", "http://example.test")
	out.Reset()
	errb.Reset()
	if code := Run([]string{"svc", "acat:widget", "list", "--dry-run"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("svc acat:widget list exit = %d, want %d (stderr: %s)", code, agentsafety.ExitOK, errb.String())
	}
}

// TestLintDoctorQualifiedAndAmbiguousSelector: `lint`/`doctor` resolve a
// qualified "<catalog>:<service>" selector (works even though the bare name is
// ambiguous), and report the ambiguity error (exit 2, listing both qualified
// forms) for the bare name instead of a misleading "unknown service".
func TestLintDoctorQualifiedAndAmbiguousSelector(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	srcA := filepath.Join(t.TempDir(), "acat")
	writeSourceManifest(t, srcA, "widget.yaml", portableWidget)
	writeCatalogIndex(t, srcA, "acat")
	srcB := filepath.Join(t.TempDir(), "bcat")
	writeSourceManifest(t, srcB, "widget.yaml", portableWidget)
	writeCatalogIndex(t, srcB, "bcat")
	var out, errb bytes.Buffer
	for _, src := range []string{srcA, srcB} {
		out.Reset()
		errb.Reset()
		if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
			t.Fatalf("add %s exit = %d (stderr: %s)", src, code, errb.String())
		}
	}

	for _, cmd := range []string{"lint", "doctor"} {
		t.Run(cmd, func(t *testing.T) {
			out.Reset()
			errb.Reset()
			if code := Run([]string{cmd, "acat:widget"}, &out, &errb); code != agentsafety.ExitOK {
				t.Fatalf("%s acat:widget exit = %d, want %d (stderr: %s)", cmd, code, agentsafety.ExitOK, errb.String())
			}

			out.Reset()
			errb.Reset()
			if code := Run([]string{cmd, "widget"}, &out, &errb); code != agentsafety.ExitUsage {
				t.Fatalf("%s widget exit = %d, want %d (usage) (stderr: %s)", cmd, code, agentsafety.ExitUsage, errb.String())
			}
			if !bytes.Contains(errb.Bytes(), []byte("acat:widget")) || !bytes.Contains(errb.Bytes(), []byte("bcat:widget")) {
				t.Errorf("%s widget stderr = %q, want it to list both qualified forms", cmd, errb.String())
			}
		})
	}
}

// TestCatalogRemove: remove deletes the catalog; removing a missing one errors.
func TestCatalogRemove(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "mycat")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeCatalogIndex(t, src, "mycat")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d (stderr: %s)", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "remove", "mycat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("remove exit = %d (stderr: %s)", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "mycat")); !os.IsNotExist(err) {
		t.Error("catalog dir should be gone after remove")
	}

	// Removing again is an error (exit 2 — *ConfigError).
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "remove", "mycat"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("remove-again exit = %d, want %d (usage)", code, agentsafety.ExitUsage)
	}
}

// TestCatalogUpdateDirSource: update re-reads a dir source and picks up a changed
// manifest.
func TestCatalogUpdateDirSource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "mycat")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeCatalogIndex(t, src, "mycat")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d (stderr: %s)", code, errb.String())
	}

	// Change the source manifest's description, then update.
	const changed = `name: widget
description: an UPDATED widget
auth: { strategy: none }
commands:
  list: { method: GET, path: /list }
`
	writeSourceManifest(t, src, "widget.yaml", changed)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update", "mycat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("update exit = %d (stderr: %s)", code, errb.String())
	}
	got, err := os.ReadFile(filepath.Join(cfg, "catalogs", "mycat", "widget.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("UPDATED")) {
		t.Errorf("update did not pick up the changed source manifest:\n%s", got)
	}
}

// TestCatalogUpdateOpenAPISource: `catalog update <name>` on an openapi-sourced
// catalog re-fetches meta.Source and re-materializes the manifest via the same
// pipeline `catalog add --openapi` uses, picking up an upstream spec change.
func TestCatalogUpdateOpenAPISource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)

	specPath := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(specPath, []byte(openapiPetstoreFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", specPath, "--openapi", "--name", "petstore"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}

	// Change the spec's info.title (still slugifies to a name the installed
	// catalog's directory does NOT control — meta.Name must win over any
	// re-inferred name) and add a new operation, then update.
	const changedSpec = `openapi: "3.0.3"
info:
  title: Pet Store v2
  version: "1.0"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets, now with more detail
      responses:
        "200": { description: ok }
  /pets/{id}:
    get:
      operationId: getPet
      summary: Get one pet
      responses:
        "200": { description: ok }
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-Api-Key
security:
  - ApiKeyAuth: []
`
	if err := os.WriteFile(specPath, []byte(changedSpec), 0o600); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update", "petstore"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("update exit = %d, want 0 (stderr: %s)", code, errb.String())
	}

	manifestPath := filepath.Join(cfg, "catalogs", "petstore", "petstore.yaml")
	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("catalog manifest missing after update: %v", err)
	}
	// The new operation was picked up...
	if !bytes.Contains(got, []byte("getpet")) && !bytes.Contains(got, []byte("get-pet")) {
		t.Errorf("update did not re-materialize the new operation:\n%s", got)
	}
	// ...but the catalog/install NAME stayed "petstore" — the directory is the
	// source of truth, not the spec's (changed) info.title.
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "petstore")); err != nil {
		t.Errorf("catalog dir should still be 'petstore' after update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "pet-store-v2")); err == nil {
		t.Error("update must not retarget the install to a re-inferred name")
	}
}

// TestCatalogUpdateBulkWithOpenAPISource: a bulk `catalog update` (no name)
// updates an openapi-sourced catalog alongside a dir-sourced one without the
// openapi case poisoning the exit code or aborting the other catalog's update
// (the existing per-catalog firstErr pattern).
func TestCatalogUpdateBulkWithOpenAPISource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)

	dirSrc := filepath.Join(t.TempDir(), "dircat")
	writeSourceManifest(t, dirSrc, "widget.yaml", portableWidget)
	writeCatalogIndex(t, dirSrc, "dircat")

	specPath := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(specPath, []byte(openapiPetstoreFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", dirSrc, "--name", "dircat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add dircat exit = %d (stderr: %s)", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "add", specPath, "--openapi", "--name", "apicat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add apicat exit = %d (stderr: %s)", code, errb.String())
	}

	// Change both sources.
	const changedWidget = `name: widget
description: an UPDATED widget
auth: { strategy: none }
commands:
  list: { method: GET, path: /list }
`
	writeSourceManifest(t, dirSrc, "widget.yaml", changedWidget)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("bulk update exit = %d, want 0 (an openapi catalog must not poison the exit code) (stderr: %s)", code, errb.String())
	}

	gotWidget, err := os.ReadFile(filepath.Join(cfg, "catalogs", "dircat", "widget.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(gotWidget, []byte("UPDATED")) {
		t.Errorf("bulk update did not refresh dircat:\n%s", gotWidget)
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "apicat", "apicat.yaml")); err != nil {
		t.Errorf("bulk update should leave apicat installed: %v", err)
	}
}

// TestCatalogUpdateOpenAPIMovedFileErrors: when the recorded local-file source
// no longer exists, update fails with the fetch error (not a panic) — the same
// failure shape as a git remote that has gone away.
func TestCatalogUpdateOpenAPIMovedFileErrors(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)

	specPath := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(specPath, []byte(openapiPetstoreFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", specPath, "--openapi", "--name", "petstore"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d (stderr: %s)", code, errb.String())
	}

	if err := os.Remove(specPath); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update", "petstore"}, &out, &errb); code == agentsafety.ExitOK {
		t.Fatalf("update should fail when the recorded local-file source no longer exists (stderr: %s)", errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("petstore")) {
		t.Errorf("stderr = %q, want it to mention the catalog %q", errb.String(), "petstore")
	}
}

// TestCatalogAddRejectsInvalidGitURL: a source that is neither an existing dir nor
// a valid git URL is a usage error (no process spawned).
func TestCatalogAddRejectsInvalidGitURL(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	for _, src := range []string{"ext::sh -c whoami", "-oProxyCommand=evil", "not a url"} {
		t.Run(src, func(t *testing.T) {
			var out, errb bytes.Buffer
			// `--name x --` first, then the source after `--` so a leading-dash
			// source is treated as a positional arg, not a flag, and reaches the
			// URL validation (which rejects it).
			if code := Run([]string{"catalog", "add", "--name", "x", "--", src}, &out, &errb); code != agentsafety.ExitUsage {
				t.Fatalf("exit = %d, want %d (usage) for source %q (stderr: %s)", code, agentsafety.ExitUsage, src, errb.String())
			}
		})
	}
}

// TestCatalogAddGitSource: a file:// git source clones, pins to the HEAD commit,
// installs, and records the commit SHA (reported by `catalog installed`). Skipped
// when git is unavailable.
func TestCatalogAddGitSource(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	// Build a tiny local git repo holding one portable manifest and its index.
	repo := t.TempDir()
	writeSourceManifest(t, repo, "widget.yaml", portableWidget)
	writeCatalogIndex(t, repo, "gitcat")
	gitInit := func(args ...string) {
		t.Helper()
		c := exec.Command(gitBin, args...)
		c.Dir = repo
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	gitInit("init", "--quiet", "-b", "main")
	gitInit("add", ".")
	gitInit("commit", "--quiet", "-m", "init")

	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", "file://" + repo, "--name", "gitcat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("git add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "gitcat", "widget.yaml")); err != nil {
		t.Fatalf("git catalog manifest not installed: %v", err)
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("catalog list exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("gitcat")) {
		t.Errorf("`catalog list` should list the git catalog:\n%s", out.String())
	}
	// `catalog info` shows the git provenance (type + pinned commit).
	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "info", "gitcat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("catalog info exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("git")) || !bytes.Contains(out.Bytes(), []byte("commit:")) {
		t.Errorf("`catalog info` should report the git type and pinned commit:\n%s", out.String())
	}
}

// TestCatalogAddInvalidNameFlag: an explicit --name that is not a valid path
// segment is a usage error and nothing is installed (basename inference is gone;
// the index or --name is the identity source).
func TestCatalogAddInvalidNameFlag(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "srcdir")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeCatalogIndex(t, src, "ok")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src, "--name", "Bad.Name"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "ok")); !os.IsNotExist(err) {
		t.Error("nothing should be installed when --name is invalid")
	}
}

// TestCatalogAddExplicitMembers: an index with a curated `manifests:` list
// installs exactly those files (and only those), in order, ignoring unlisted
// manifests in the source.
func TestCatalogAddExplicitMembers(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "srcdir")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeSourceManifest(t, src, "gadget.yaml", strings.Replace(portableWidget, "name: widget", "name: gadget", 1))
	// Only widget is a member; gadget is present but unlisted.
	writeCatalogIndex(t, src, "curated", "widget.yaml")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "curated", "widget.yaml")); err != nil {
		t.Errorf("the listed member widget.yaml should be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "curated", "gadget.yaml")); !os.IsNotExist(err) {
		t.Error("an unlisted manifest must not be installed")
	}
}

// TestCatalogAddMissingMemberFailsClosed: an index listing a manifest that does
// not exist fails the whole add (fail-closed); nothing is installed.
func TestCatalogAddMissingMemberFailsClosed(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "srcdir")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeCatalogIndex(t, src, "curated", "widget.yaml", "ghost.yaml")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code == agentsafety.ExitOK {
		t.Fatalf("add should fail when a listed member is missing (stderr: %s)", errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "curated")); !os.IsNotExist(err) {
		t.Error("nothing should be installed when a listed member is missing")
	}
}
