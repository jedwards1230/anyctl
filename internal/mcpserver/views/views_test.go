package views

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResultHTMLEmbeddedDefault proves the embedded copy is served when
// ANYCTL_VIEWS_DIR is unset, and that it is non-empty, well-formed-enough
// stub HTML.
func TestResultHTMLEmbeddedDefault(t *testing.T) {
	t.Setenv("ANYCTL_VIEWS_DIR", "")
	got := string(ResultHTML())
	if got == "" {
		t.Fatal("ResultHTML() returned empty bytes")
	}
	if !strings.Contains(got, "<html") {
		t.Errorf("ResultHTML() = %q, want it to contain <html", got)
	}
}

// TestResultHTMLViewsDirOverride proves ANYCTL_VIEWS_DIR, when set and
// readable, overrides the embedded copy — the dev loop for iterating on
// views/ without a Go rebuild.
func TestResultHTMLViewsDirOverride(t *testing.T) {
	dir := t.TempDir()
	want := "<!doctype html><html><body>override</body></html>"
	if err := os.WriteFile(filepath.Join(dir, "result.html"), []byte(want), 0o644); err != nil {
		t.Fatalf("write override result.html: %v", err)
	}
	t.Setenv("ANYCTL_VIEWS_DIR", dir)

	got := string(ResultHTML())
	if got != want {
		t.Errorf("ResultHTML() = %q, want override %q", got, want)
	}
}

// TestResultHTMLLegacyViewsDirOverride proves the legacy LABCTL_VIEWS_DIR name
// still overrides the embedded copy (back-compat), and that the new name wins
// when both are set.
func TestResultHTMLLegacyViewsDirOverride(t *testing.T) {
	legacyDir := t.TempDir()
	legacyWant := "<!doctype html><html><body>legacy</body></html>"
	if err := os.WriteFile(filepath.Join(legacyDir, "result.html"), []byte(legacyWant), 0o644); err != nil {
		t.Fatalf("write legacy override: %v", err)
	}
	t.Setenv("ANYCTL_VIEWS_DIR", "")
	t.Setenv("LABCTL_VIEWS_DIR", legacyDir)
	if got := string(ResultHTML()); got != legacyWant {
		t.Errorf("ResultHTML() = %q, want legacy override %q", got, legacyWant)
	}

	newDir := t.TempDir()
	newWant := "<!doctype html><html><body>new</body></html>"
	if err := os.WriteFile(filepath.Join(newDir, "result.html"), []byte(newWant), 0o644); err != nil {
		t.Fatalf("write new override: %v", err)
	}
	t.Setenv("ANYCTL_VIEWS_DIR", newDir)
	if got := string(ResultHTML()); got != newWant {
		t.Errorf("ResultHTML() = %q, want new override %q (new name preferred)", got, newWant)
	}
}

// TestResultHTMLViewsDirMissingFileFallsBack proves an unreadable override
// (dir set but no result.html in it) falls back to the embedded copy rather
// than erroring or serving an empty body.
func TestResultHTMLViewsDirMissingFileFallsBack(t *testing.T) {
	t.Setenv("ANYCTL_VIEWS_DIR", t.TempDir()) // empty dir, no result.html
	got := string(ResultHTML())
	if got != string(embeddedResultHTML) {
		t.Errorf("ResultHTML() with missing override file = %q, want embedded fallback", got)
	}
}
