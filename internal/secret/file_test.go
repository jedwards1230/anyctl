package secret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeSecretFile writes content at a fresh temp path with the given mode.
func writeSecretFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	// os.WriteFile is subject to umask; force the exact mode.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileResolve(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"trailing newline trimmed", "s3kret\n", "s3kret"},
		{"trailing whitespace trimmed", "s3kret \t\r\n", "s3kret"},
		{"no trailing whitespace", "s3kret", "s3kret"},
		{"leading whitespace preserved", "  s3kret", "  s3kret"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeSecretFile(t, c.content, 0o600)
			p := newFile("file")
			v, err := p.Resolve(context.Background(), Ref{URI: "file://" + path})
			if err != nil {
				t.Fatal(err)
			}
			if v != c.want {
				t.Fatalf("Resolve = %q, want %q", v, c.want)
			}
		})
	}
}

// TestFileRejectsLoosePerms asserts a file readable by group/other is refused
// with an *AuthError naming the mode — the same guard the SA-token file uses.
func TestFileRejectsLoosePerms(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o660, 0o777} {
		path := writeSecretFile(t, "s3kret\n", mode)
		p := newFile("file")
		_, err := p.Resolve(context.Background(), Ref{URI: "file://" + path})
		if err == nil {
			t.Fatalf("mode %#o: expected a permission rejection", mode)
		}
		var authErr *AuthError
		if !errors.As(err, &authErr) {
			t.Fatalf("mode %#o: error %v is not an *AuthError", mode, err)
		}
	}
}

func TestFileAcceptsOwnerOnlyModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		path := writeSecretFile(t, "s3kret\n", mode)
		p := newFile("file")
		v, err := p.Resolve(context.Background(), Ref{URI: "file://" + path})
		if err != nil {
			t.Fatalf("mode %#o: unexpected error %v", mode, err)
		}
		if v != "s3kret" {
			t.Fatalf("mode %#o: got %q", mode, v)
		}
	}
}

func TestFileMissingIsAuthError(t *testing.T) {
	p := newFile("file")
	_, err := p.Resolve(context.Background(), Ref{URI: "file:///nonexistent/anyctl/secret"})
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error %v is not an *AuthError", err)
	}
}

func TestFileRejectsItemIdiom(t *testing.T) {
	path := writeSecretFile(t, "s3kret\n", 0o600)
	p := newFile("file")
	_, err := p.Resolve(context.Background(), Ref{URI: "file://" + path, Idiom: "item-json"})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError for an item idiom on a file provider, got %v", err)
	}
}

func TestFileRejectsPathlessRef(t *testing.T) {
	p := newFile("file")
	for _, ref := range []string{"file://", "op://vault/item/field"} {
		_, err := p.Resolve(context.Background(), Ref{URI: ref})
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("ref %q: expected *ConfigError, got %v", ref, err)
		}
	}
}

func TestFileSchemeDefaults(t *testing.T) {
	if s := newFile("").Scheme(); s != "file" {
		t.Fatalf("newFile(\"\").Scheme() = %q, want file", s)
	}
	if s := newFile("sops").Scheme(); s != "sops" {
		t.Fatalf("newFile(\"sops\").Scheme() = %q, want sops", s)
	}
}
