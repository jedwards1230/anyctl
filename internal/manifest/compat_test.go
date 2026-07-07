package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// clearConfigEnv unsets every env var ConfigDir consults so a test drives the
// resolution deterministically.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ANYCTL_CONFIG_DIR", "")
	t.Setenv("LABCTL_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestConfigDirPrefersAnyctlEnv(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", "/new/dir")
	t.Setenv("LABCTL_CONFIG_DIR", "/old/dir")
	if got := ConfigDir(); got != "/new/dir" {
		t.Fatalf("ConfigDir = %q, want the ANYCTL_CONFIG_DIR value", got)
	}
}

func TestConfigDirLegacyEnvFallback(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", "")
	t.Setenv("LABCTL_CONFIG_DIR", "/legacy/dir")
	if got := ConfigDir(); got != "/legacy/dir" {
		t.Fatalf("ConfigDir = %q, want the legacy LABCTL_CONFIG_DIR value", got)
	}
}

func TestConfigDirLegacyDirFallback(t *testing.T) {
	clearConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacy := filepath.Join(home, ".config", "labctl")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	// ~/.config/anyctl does NOT exist → legacy dir is used.
	if got := ConfigDir(); got != legacy {
		t.Fatalf("ConfigDir = %q, want the legacy dir %q", got, legacy)
	}
}

func TestConfigDirPrefersNewDirWhenBothExist(t *testing.T) {
	clearConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	newDir := filepath.Join(home, ".config", "anyctl")
	legacy := filepath.Join(home, ".config", "labctl")
	for _, d := range []string{newDir, legacy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := ConfigDir(); got != newDir {
		t.Fatalf("ConfigDir = %q, want the new dir %q when both exist", got, newDir)
	}
}

func TestConfigDirDefaultsToAnyctl(t *testing.T) {
	clearConfigEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "anyctl")
	if got := ConfigDir(); got != want {
		t.Fatalf("ConfigDir = %q, want %q", got, want)
	}
}

func TestReadMetaFileAcceptsLegacyMarker(t *testing.T) {
	dir := t.TempDir()
	meta := CatalogMeta{Name: "demo", Type: "dir", AddedAt: time.Now(), UpdatedAt: time.Now()}
	// Write ONLY the legacy marker, as an old labctl binary would have.
	if err := writeMetaFileNamed(t, dir, LegacyCatalogMetaFile, meta); err != nil {
		t.Fatal(err)
	}
	got, found, err := readMetaFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("readMetaFile found=false, want it to accept the legacy marker")
	}
	if got.Name != "demo" {
		t.Fatalf("meta.Name = %q, want demo", got.Name)
	}
}

func TestReadMetaFilePrefersCurrentMarker(t *testing.T) {
	dir := t.TempDir()
	if err := writeMetaFileNamed(t, dir, LegacyCatalogMetaFile, CatalogMeta{Name: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaFileNamed(t, dir, CatalogMetaFile, CatalogMeta{Name: "current"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := readMetaFile(dir)
	if err != nil || !found {
		t.Fatalf("readMetaFile found=%v err=%v", found, err)
	}
	if got.Name != "current" {
		t.Fatalf("meta.Name = %q, want current (the new marker wins)", got.Name)
	}
}

func writeMetaFileNamed(t *testing.T, dir, name string, meta CatalogMeta) error {
	t.Helper()
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), b, 0o600)
}
