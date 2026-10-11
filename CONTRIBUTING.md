# Contributing to anyctl

`anyctl` is a manifest-driven Go CLI for HTTP/RPC service APIs. All changes go through the workflow below.

## Prerequisites

- Go 1.26 or later (see `go.mod` for the exact floor version)

## Build, test & lint

```bash
# Build
go build -o anyctl .

# Format check (must be clean before committing)
gofmt -l .

# Vet
go vet ./...

# Lint (required by CI)
golangci-lint run

# Test with race detector (CI requires ≥75% coverage)
go test -race ./...

# Module hygiene (go.mod and go.sum must stay tidy)
go mod tidy && git diff --exit-code go.mod go.sum

# Full build smoke test
go build ./...

# Try the minimal quickstart (one no-auth service, runs against httpbin.org)
ANYCTL_CONFIG_DIR="$PWD/examples/quickstart" ./anyctl lint
ANYCTL_CONFIG_DIR="$PWD/examples/quickstart" ./anyctl --dry-run svc httpbin get

# Try the full config: an installed reference catalog + a profile binding it
ANYCTL_CONFIG_DIR="$PWD/examples/full" ./anyctl list          # 2 services, origin catalog:reference
ANYCTL_CONFIG_DIR="$PWD/examples/full" ./anyctl lint --strict
ANYCTL_CONFIG_DIR="$PWD/examples/full" ./anyctl --dry-run svc uptime status

# Validate the reference catalog (read-only, no config dir needed)
./anyctl catalog validate examples/catalog
```

CI runs `gofmt`, `go vet`, `golangci-lint`, `go mod tidy` check, `go test -race` (with a 75% coverage floor), and `go build`. All checks must pass before a PR can merge.

If you change `views/`, rebuild and commit `internal/mcpserver/views/result.html` in the same PR (`cd views && npm ci && npm run build`). `views/` is dev-only for releases, so the committed `result.html` is what ships; the `Views` workflow fails the PR if it is stale.

## Adding or authoring a service manifest

anyctl ships no built-in manifests — a service is a local `services/<name>.yaml`
or a manifest inside an installed catalog. Editing YAML is **rebuild-free**:

1. **Scaffold a starter manifest** into a config dir's `services/`:
   ```bash
   export ANYCTL_CONFIG_DIR=$(mktemp -d)
   anyctl init <name> --auth bearer -o "$ANYCTL_CONFIG_DIR/services/<name>.yaml"
   ```
2. **Edit and test** (bind its `base_url`/secrets in `$ANYCTL_CONFIG_DIR/profile.yaml`):
   ```bash
   $EDITOR "$ANYCTL_CONFIG_DIR/services/<name>.yaml"
   anyctl lint --strict <name>              # structural + completeness check
   anyctl svc <name> <command> --dry-run    # preview the resolved request without sending
   ```
3. **Publish a catalog** (optional): a git repo (any forge) or directory with an
   `anyctl-catalog.yaml` index plus *portable* manifests is an installable
   catalog. Validate it against anyctl's contract before anyone installs it:
   ```bash
   anyctl catalog validate ./my-catalog   # read-only: index + schema + portability check
   ```
   The reference catalog under [`examples/catalog/`](examples/catalog/) — index
   plus two manifests — is the template a third-party author copies.

## Documentation

Keep documentation current as part of the change, not as a follow-up — update the README and any affected docs in the same PR.

## Before you open a PR

- Make sure all CI checks pass locally first — run the formatter, linter, and tests.

## Branching & commits

- Branch off `main`; never commit directly to `main`.
- Use [Conventional Commits](https://www.conventionalcommits.org/) prefixes (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, …).
- Sign your commits where possible (`git commit -S`).
- Keep each PR focused; delete dead code rather than commenting it out.

## Pull requests

- Open the PR against `main`.
- Every PR runs CI (required check: **Test & Lint**). Resolve **all** review threads before the PR is merged.
- An automated code review runs on each PR; address and resolve its threads like any other review.
- A PR can be merged once CI is green and all review threads are resolved.

## Releases

Releases are decided per merged PR:

- An explicit `semver:major`, `semver:minor`, or `semver:patch` label always wins and sets the bump (the highest one if several); `semver:none` skips the release when it is the only `semver:*` label.
- With no label, the PR releases as a **patch** unless every changed file is dev-only (CI workflows, `CLAUDE.md`/`AGENTS.md`, `CONTRIBUTING*`, tests and `testdata/` fixtures, linter config, and the `views/` build sources). The list lives in [`.github/.releaseignore`](.github/.releaseignore) in gitignore syntax (note git's rule: a file can't be re-included if a parent directory is excluded, so re-include a directory with `!dir/` before `!dir/**`); edit that file to change what counts. Dependabot bumps to GitHub Actions or `views/` therefore do not release; Go module and Docker base-image bumps do.
- If the files can't be classified (direct push with no associated PR, or with no label a file list that can't be fetched, is empty, or fails to classify), the merge releases as a patch and logs a `::warning::`. A lone `semver:none` label is still honored.
- A `semver:*` bump label on a dev-only diff still releases but logs a warning suggesting `semver:none`.

A release publishes a single immutable `vX.Y.Z` tag with AI-generated release notes and cross-compiled static binaries for Linux and macOS (amd64 + arm64).
