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
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestConfigDirPrefersAnyctlEnv(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", "/new/dir")
	if got := ConfigDir(); got != "/new/dir" {
		t.Fatalf("ConfigDir = %q, want the ANYCTL_CONFIG_DIR value", got)
	}
}

func TestConfigDirPrefersXDG(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	want := filepath.Join("/xdg", "anyctl")
	if got := ConfigDir(); got != want {
		t.Fatalf("ConfigDir = %q, want %q", got, want)
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

func TestReadMetaFileReadsCurrentMarker(t *testing.T) {
	dir := t.TempDir()
	meta := CatalogMeta{Name: "demo", Type: "dir", AddedAt: time.Now(), UpdatedAt: time.Now()}
	if err := writeMetaFileNamed(t, dir, CatalogMetaFile, meta); err != nil {
		t.Fatal(err)
	}
	got, found, err := readMetaFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("readMetaFile found=false, want it to read the marker")
	}
	if got.Name != "demo" {
		t.Fatalf("meta.Name = %q, want demo", got.Name)
	}
}

func TestReadMetaFileAbsentIsNotFound(t *testing.T) {
	dir := t.TempDir()
	_, found, err := readMetaFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("readMetaFile found=true for an empty dir, want false")
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
