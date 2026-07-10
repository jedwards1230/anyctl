package secret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jedwards1230/anyctl/internal/manifest"
)

// Exec resolves refs via an arbitrary {ref}-templated command, e.g.
// `["pass", "show", "{ref}"]`, `["vault", "read", "-field=value", "{ref}"]`, or a
// `sops` invocation. It is the op provider minus 1Password's item idioms and
// service-account-token injection: a config-only backend for any scheme whose
// value comes from a subprocess's stdout. The scheme comes from config, so a
// `pass://` or `vault://` provider needs no new Go. The resolved value never
// appears in argv (it is the command's OUTPUT) and is never written to any writer.
type Exec struct {
	scheme  string   // URI scheme this provider handles (from config)
	command []string // argv with {ref} placeholder
	run     Runner   // non-nil only in tests; nil = real exec
}

// newExec builds a generic exec provider for scheme from its config. Side-effect
// free: it reads no files and runs no command (that is lazy, on Resolve).
func newExec(scheme string, cfg manifest.ProviderConfig, runner Runner) *Exec {
	return &Exec{
		scheme:  scheme,
		command: append([]string(nil), cfg.Command...),
		run:     runner,
	}
}

// Scheme returns the configured scheme.
func (p *Exec) Scheme() string { return p.scheme }

// ResolvedBinary resolves the absolute path of command[0] via exec.LookPath for
// --dry-run/--verbose audit visibility only — no invocation, never a gate (a
// lookup failure is reported by the caller as unresolved, not a hard failure).
func (p *Exec) ResolvedBinary() (string, error) {
	if len(p.command) == 0 || p.command[0] == "" {
		return "", fmt.Errorf("resolver command is empty")
	}
	return exec.LookPath(p.command[0])
}

// Resolve runs the configured command with {ref} substituted, honoring the
// field fallback. Only the default "read" idiom is supported — item-get/item-json
// are 1Password-specific, so a manifest asking for them against an exec provider
// is a config mistake (*ConfigError → exit 2). An empty command is likewise a
// config mistake. ctx carries cancellation/deadline into the subprocess.
func (p *Exec) Resolve(ctx context.Context, ref Ref) (string, error) {
	if idiom := ref.Idiom; idiom != "" && idiom != "read" {
		return "", &ConfigError{Err: fmt.Errorf(
			"%q provider supports only the \"read\" idiom, not %q (item-get/item-json are 1Password-specific)", p.scheme, idiom)}
	}
	if p.run == nil && (len(p.command) == 0 || p.command[0] == "") {
		return "", &ConfigError{Err: fmt.Errorf("%q provider has no command; set secrets.providers.%s.command in config.yaml", p.scheme, p.scheme)}
	}
	return resolveTemplatedRead(ref, p.command, func(argv []string) (string, error) {
		if p.run != nil {
			return p.run(argv)
		}
		return runResolverCommand(ctx, argv, "", execBinaryDesc(p.scheme, p.command))
	})
}

// resolveTemplatedRead runs a {ref}-templated command with the ordered field
// fallback: with no fields it tries the ref once; with fields it replaces the
// ref's final segment with each candidate until one returns non-empty. An
// all-empty result returns ("", nil); the caller maps that to "resolved empty".
// exec runs one substituted argv. Shared by the op and exec providers.
func resolveTemplatedRead(ref Ref, command []string, exec func(argv []string) (string, error)) (string, error) {
	refs := []string{ref.URI}
	if len(ref.Fields) > 0 {
		refs = refs[:0]
		for _, f := range ref.Fields {
			refs = append(refs, replaceLastSegment(ref.URI, f))
		}
	}
	var lastErr error
	for _, r := range refs {
		out, err := exec(substituteRef(command, r))
		if err != nil {
			lastErr = err
			continue
		}
		if out != "" {
			return out, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", nil
}

// runResolverCommand runs a resolver argv and returns its trimmed stdout. tok, when
// non-empty, is injected as OP_SERVICE_ACCOUNT_TOKEN into the child env only (the op
// provider; empty for token-free providers) — never argv, never any writer. desc
// labels the backing binary in a missing-binary diagnostic. ctx cancellation kills
// the subprocess.
func runResolverCommand(ctx context.Context, argv []string, tok, desc string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty resolver command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv is the manifest-configured secret resolver (e.g. `op`), not attacker input
	cmd.Stderr = os.Stderr                                // let the tool print its own diagnostics (session expired, etc.)
	if tok != "" {
		// cmd.Env replaces the whole environment, so start from os.Environ().
		cmd.Env = withToken(os.Environ(), tok)
	}
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", missingBinaryError(desc, argv[0])
		}
		return "", fmt.Errorf("%s: %w", argv[0], err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// execBinaryDesc labels an exec provider's backing binary for a missing-binary
// diagnostic, e.g. `the "pass" secrets provider (pass)`.
func execBinaryDesc(scheme string, command []string) string {
	bin := "the resolver"
	if len(command) > 0 && command[0] != "" {
		bin = command[0]
	}
	return fmt.Sprintf("the %q secrets provider (%s)", scheme, bin)
}

// missingBinaryError builds an actionable "binary not on PATH" error: it names
// the provider, points at the <PREFIX>_<SECRET> env-override escape hatch (which
// skips provider resolution entirely), and notes providers are configurable in
// config.yaml. Kept a plain error so the resolver's provider-failure wrap still
// classifies it as a credential failure (exit 3), matching the op path.
func missingBinaryError(desc, bin string) error {
	return fmt.Errorf(
		"%s not found on PATH (looked for %q): install it, or set the per-secret env override <PREFIX>_<SECRET> to supply the value directly (env overrides skip provider resolution entirely). Providers are configured under secrets.providers in config.yaml",
		desc, bin,
	)
}
