package secret

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// File resolves file:// references by reading the secret from a file on disk.
// The ref path is everything after "file://" (a leading ~ or $HOME is expanded),
// so file:///run/secrets/token reads /run/secrets/token. Trailing whitespace is
// trimmed. The file must be owner-only (mode & 0077 == 0, e.g. 0600/0400) — the
// same permission check the 1Password service-account-token file enforces —
// because a plaintext secret on disk is only as safe as its mode. Handy for
// Kubernetes/Docker secret mounts and CI files, with zero external binary.
type File struct {
	scheme string // URI scheme this provider handles (default "file")
}

// newFile builds a file provider for scheme (typically "file").
func newFile(scheme string) *File {
	if scheme == "" {
		scheme = "file"
	}
	return &File{scheme: scheme}
}

// Scheme returns the configured scheme.
func (p *File) Scheme() string { return p.scheme }

// Resolve reads the secret from the ref's file path. Only the default "read"
// idiom is supported (item-get/item-json are 1Password-specific). The field
// fallback does not apply — a file holds one value. ctx is unused (a local read).
func (p *File) Resolve(_ context.Context, ref Ref) (string, error) {
	if idiom := ref.Idiom; idiom != "" && idiom != "read" {
		return "", &ConfigError{Err: fmt.Errorf(
			"%q provider supports only the \"read\" idiom, not %q", p.scheme, idiom)}
	}
	raw := strings.TrimPrefix(ref.URI, p.scheme+"://")
	if raw == "" || raw == ref.URI {
		return "", &ConfigError{Err: fmt.Errorf("%q ref %q must be %s://<path>", p.scheme, ref.URI, p.scheme)}
	}
	path, err := expandHome(raw)
	if err != nil {
		return "", &AuthError{Err: fmt.Errorf("%s: %w", p.scheme, err)}
	}
	if err := checkOwnerOnlyPerms(path, p.scheme); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", &AuthError{Err: fmt.Errorf("%s: read %s: %w", p.scheme, path, err)}
	}
	return strings.TrimRight(string(b), " \t\r\n"), nil
}
