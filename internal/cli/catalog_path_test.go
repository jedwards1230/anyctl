package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// gitRepoRunner returns a helper that runs git in repo with a fixed identity, for
// building local file:// fixtures. It fatals on any git error.
func gitRepoRunner(t *testing.T, gitBin, repo string) func(args ...string) {
	t.Helper()
	return func(args ...string) {
		t.Helper()
		c := exec.Command(gitBin, args...)
		c.Dir = repo
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// TestCatalogAddGitSubdir: a git source with --path installs manifests from the
// named subdirectory (ignoring anything at the repo root), records the subdir in
// the catalog metadata, and its services load with catalog:<name> provenance.
func TestCatalogAddGitSubdir(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	// A subdir holds the catalog; the repo root holds an unrelated (non-manifest)
	// file that must be ignored when --path points at the subdir.
	writeSourceManifest(t, filepath.Join(repo, "anyctl-catalog"), "widget.yaml", portableWidget)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# infra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git := gitRepoRunner(t, gitBin, repo)
	git("init", "--quiet", "-b", "main")
	git("add", ".")
	git("commit", "--quiet", "-m", "init")

	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", "file://" + repo, "--path", "anyctl-catalog", "--name", "infra"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "infra", "widget.yaml")); err != nil {
		t.Fatalf("subdir manifest not installed: %v", err)
	}
	// The recorded metadata pins the subdir so `catalog update` re-fetches it.
	metaBytes, err := os.ReadFile(filepath.Join(cfg, "catalogs", "infra", ".anyctl-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(metaBytes, []byte(`"path": "anyctl-catalog"`)) {
		t.Errorf("catalog metadata should record the subdir path:\n%s", metaBytes)
	}

	// Services load with catalog provenance.
	out.Reset()
	errb.Reset()
	if code := Run([]string{"list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("list exit = %d (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("catalog:infra")) {
		t.Errorf("list should mark widget as catalog:infra:\n%s", out.String())
	}
}

// TestCatalogAddGitSubdirTraversalRejected: --path must be a relative path within
// the clone. An absolute path or a `..` escaping the repo is a usage error and
// nothing is installed.
func TestCatalogAddGitSubdirTraversalRejected(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	writeSourceManifest(t, filepath.Join(repo, "sub"), "widget.yaml", portableWidget)
	git := gitRepoRunner(t, gitBin, repo)
	git("init", "--quiet", "-b", "main")
	git("add", ".")
	git("commit", "--quiet", "-m", "init")

	for _, bad := range []string{"/etc", "../escape", "sub/../../escape"} {
		t.Run(bad, func(t *testing.T) {
			cfg := t.TempDir()
			t.Setenv("ANYCTL_CONFIG_DIR", cfg)
			var out, errb bytes.Buffer
			// `--` guards a leading-dash value from being read as a flag.
			if code := Run([]string{"catalog", "add", "file://" + repo, "--name", "infra", "--path", bad, "--"}, &out, &errb); code != agentsafety.ExitUsage {
				t.Fatalf("add --path %q exit = %d, want %d (usage) (stderr: %s)", bad, code, agentsafety.ExitUsage, errb.String())
			}
			if _, err := os.Stat(filepath.Join(cfg, "catalogs", "infra")); !os.IsNotExist(err) {
				t.Errorf("nothing should be installed for a traversing --path %q", bad)
			}
		})
	}
}

// TestCatalogAddGitSubdirMissing: pointing --path at a subdir that does not exist
// in the repo is a clear usage error, and nothing is installed.
func TestCatalogAddGitSubdirMissing(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	writeSourceManifest(t, repo, "widget.yaml", portableWidget)
	git := gitRepoRunner(t, gitBin, repo)
	git("init", "--quiet", "-b", "main")
	git("add", ".")
	git("commit", "--quiet", "-m", "init")

	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", "file://" + repo, "--name", "infra", "--path", "nope"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("add exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("not found")) {
		t.Errorf("stderr = %q, want a 'not found' diagnostic for the missing subdir", errb.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "catalogs", "infra")); !os.IsNotExist(err) {
		t.Error("nothing should be installed when the subdir is missing")
	}
}

// TestCatalogAddSubdirRejectedForDirSource: --path is git-only; a local dir source
// with --path is a usage error (point the dir source deeper instead).
func TestCatalogAddSubdirRejectedForDirSource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "mycat")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src, "--path", "sub"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("add exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("--path")) {
		t.Errorf("stderr = %q, want it to explain --path is git-only", errb.String())
	}
}

// TestCatalogAddSubdirRejectedForOpenAPI: --path cannot be combined with --openapi.
func TestCatalogAddSubdirRejectedForOpenAPI(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	specPath := filepath.Join(t.TempDir(), "petstore.yaml")
	if err := os.WriteFile(specPath, []byte(openapiPetstoreFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", specPath, "--openapi", "--name", "petstore", "--path", "sub"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("add exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("--path")) {
		t.Errorf("stderr = %q, want it to explain --path is git-only", errb.String())
	}
}

// TestCatalogUpdateGitSubdir: `catalog update` on a subdir-sourced git catalog
// re-fetches from the SAME recorded subdirectory, picking up a change committed
// there (and not confused by unrelated churn at the repo root).
func TestCatalogUpdateGitSubdir(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	subDir := filepath.Join(repo, "anyctl-catalog")
	writeSourceManifest(t, subDir, "widget.yaml", portableWidget)
	git := gitRepoRunner(t, gitBin, repo)
	git("init", "--quiet", "-b", "main")
	git("add", ".")
	git("commit", "--quiet", "-m", "init")

	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", "file://" + repo, "--path", "anyctl-catalog", "--name", "infra"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d (stderr: %s)", code, errb.String())
	}

	// Change the manifest inside the subdir and commit a new revision.
	const changed = `name: widget
description: an UPDATED widget
auth: { strategy: none }
commands:
  list: { method: GET, path: /list }
`
	writeSourceManifest(t, subDir, "widget.yaml", changed)
	git("add", ".")
	git("commit", "--quiet", "-m", "update")

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update", "infra"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("update exit = %d (stderr: %s)", code, errb.String())
	}
	got, err := os.ReadFile(filepath.Join(cfg, "catalogs", "infra", "widget.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("UPDATED")) {
		t.Errorf("update did not re-fetch from the recorded subdir:\n%s", got)
	}
}
