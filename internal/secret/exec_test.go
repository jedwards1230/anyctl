package secret

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/manifest"
)

// execProvider builds a stub-backed generic exec provider (no real subprocess).
func execProvider(scheme string, command []string, run Runner) *Exec {
	return newExec(scheme, manifest.ProviderConfig{Command: command}, run)
}

func TestExecResolveRead(t *testing.T) {
	var gotArgv []string
	p := execProvider("pass", []string{"pass", "show", "{ref}"}, func(argv []string) (string, error) {
		gotArgv = argv
		return "s3kret\n", nil
	})
	// The stub returns the value verbatim; trimming is the real exec path's job.
	v, err := p.Resolve(context.Background(), Ref{URI: "pass://vault/Radarr/api_key"})
	if err != nil {
		t.Fatal(err)
	}
	if v != "s3kret\n" {
		t.Fatalf("got %q", v)
	}
	want := []string{"pass", "show", "pass://vault/Radarr/api_key"}
	if !reflect.DeepEqual(gotArgv, want) {
		t.Fatalf("argv = %v, want %v", gotArgv, want)
	}
	if p.Scheme() != "pass" {
		t.Fatalf("Scheme() = %q, want pass", p.Scheme())
	}
}

func TestExecFieldFallback(t *testing.T) {
	p := execProvider("vault", []string{"vault", "read", "{ref}"}, func(argv []string) (string, error) {
		ref := argv[len(argv)-1]
		switch {
		case strings.HasSuffix(ref, "/credential"):
			return "", nil
		case strings.HasSuffix(ref, "/password"):
			return "pw", nil
		default:
			return "", nil
		}
	})
	v, err := p.Resolve(context.Background(), Ref{URI: "vault://a/n8n/credential", Fields: []string{"credential", "password"}})
	if err != nil {
		t.Fatal(err)
	}
	if v != "pw" {
		t.Fatalf("got %q, want pw (field fallback)", v)
	}
}

func TestExecRejectsItemIdiom(t *testing.T) {
	p := execProvider("pass", []string{"pass", "show", "{ref}"}, func([]string) (string, error) { return "x", nil })
	_, err := p.Resolve(context.Background(), Ref{URI: "pass://a/b/c", Idiom: "item-get"})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError for an item idiom on an exec provider, got %v", err)
	}
}

func TestExecEmptyCommandIsConfigError(t *testing.T) {
	// Real-exec path (nil runner) with no command is a config mistake, not a panic.
	p := execProvider("pass", nil, nil)
	_, err := p.Resolve(context.Background(), Ref{URI: "pass://a/b/c"})
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ConfigError for an empty command, got %v", err)
	}
}

// TestExecMissingBinaryActionableError exercises the real-exec path with a binary
// that is not on PATH and asserts the error is actionable: it names the provider,
// points at the <PREFIX>_<SECRET> env override, and mentions config.yaml.
func TestExecMissingBinaryActionableError(t *testing.T) {
	p := execProvider("pass", []string{"anyctl-definitely-not-a-real-binary", "{ref}"}, nil)
	_, err := p.Resolve(context.Background(), Ref{URI: "pass://a/b/c"})
	if err == nil {
		t.Fatal("expected an error for a binary not on PATH")
	}
	msg := err.Error()
	for _, want := range []string{`"pass" secrets provider`, "<PREFIX>_<SECRET>", "config.yaml", "PATH"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing-binary error %q does not mention %q", msg, want)
		}
	}
}

func TestExecResolvedBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pass")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := execProvider("pass", []string{bin, "{ref}"}, nil)
	got, err := p.ResolvedBinary()
	if err != nil {
		t.Fatalf("ResolvedBinary: %v", err)
	}
	if got != bin {
		t.Fatalf("ResolvedBinary() = %q, want %q", got, bin)
	}

	empty := execProvider("pass", nil, nil)
	if _, err := empty.ResolvedBinary(); err == nil {
		t.Fatal("expected an error for an empty resolver command")
	}
}

// TestExecTrimsTrailingWhitespace_RealPath runs a stub script (a real subprocess)
// and confirms the exec path trims the trailing newline from its stdout.
func TestExecTrimsTrailingWhitespace_RealPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "echo-secret")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'the-value\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := execProvider("pass", []string{bin, "{ref}"}, nil) // nil runner → real exec
	v, err := p.Resolve(context.Background(), Ref{URI: "pass://a/b/c"})
	if err != nil {
		t.Fatal(err)
	}
	if v != "the-value" {
		t.Fatalf("got %q, want %q (trailing newline trimmed)", v, "the-value")
	}
}

// TestRegistryDispatchExecAndFile verifies NewRegistry routes an arbitrary
// scheme to a generic exec provider and file/file-typed configs to the file
// provider, all without any new switch case per scheme.
func TestRegistryDispatchExecAndFile(t *testing.T) {
	sc := manifest.SecretsConfig{Providers: map[string]manifest.ProviderConfig{
		"pass":       {Command: []string{"pass", "show", "{ref}"}}, // scheme inferred from key → exec
		"file":       {},                                           // scheme "file" → file provider
		"myexec":     {Type: "exec", Scheme: "custom", Command: []string{"tool", "{ref}"}},
		"secretfile": {Type: "file", Scheme: "sops"},
	}}
	reg := NewRegistry(sc, func([]string) (string, error) { return "", nil })

	cases := []struct {
		scheme   string
		wantKind string // "*secret.Exec" | "*secret.File"
	}{
		{"pass", "*secret.Exec"},
		{"file", "*secret.File"},
		{"custom", "*secret.Exec"},
		{"sops", "*secret.File"},
	}
	for _, c := range cases {
		p, ok := reg.For(c.scheme)
		if !ok {
			t.Fatalf("no provider registered for scheme %q", c.scheme)
		}
		if got := providerTypeName(p); got != c.wantKind {
			t.Fatalf("scheme %q → %s, want %s", c.scheme, got, c.wantKind)
		}
	}
	if _, ok := reg.For("op"); ok {
		t.Fatal("did not expect an op provider (none configured)")
	}
}

func providerTypeName(p Provider) string {
	switch p.(type) {
	case *Exec:
		return "*secret.Exec"
	case *File:
		return "*secret.File"
	case *OnePassword:
		return "*secret.OnePassword"
	default:
		return "unknown"
	}
}
