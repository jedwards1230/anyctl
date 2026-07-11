# Plan: catalog index manifest + git marketplace + CLI polish

Branch `feat/catalog-git-marketplace`. Post-#61 baseline: no embedded floor;
resolution = local `services/` > installed `catalogs/*/`. One PR, `semver:minor`
(new feature + pre-1.0 breaking CLI).

## 1. Required catalog index: `anyctl-catalog.yaml`

Every dir/git catalog source MUST carry `anyctl-catalog.yaml` at its root (the
`--path` subdir for a subdir install). Bare-dir `*.yaml` globbing as *discovery*
dies; the index is the intentional-publish marker and identity record.

```yaml
# anyctl-catalog.yaml — at the catalog root
name: reference               # required; default install name; ^[a-z0-9][a-z0-9_-]*$
description: two-service reference catalog   # required, one line
version: "1.0.0"              # optional, informational (shown by catalog list/info)
homepage: https://github.com/you/your-catalog   # optional
manifests:                    # optional — see member mechanism
  - uptime.yaml
  - inventory.yaml
```

**Member mechanism (hybrid — required index, optional member list):**

- `manifests:` absent → auto-glob top-level `*.yaml`/`*.yml` (today's behavior),
  index file excluded.
- `manifests:` present → install exactly those files, in-repo relative bare
  filenames only; a listed file that is missing or invalid fails the whole add
  (same fail-closed gate). Unlisted files are simply not installed — curation,
  ordering, subsets.

Why required: a marketplace listing needs identity (name/description) to
display; it stops accidental installs of any random YAML dir; it gives the
author name authority; and it is the forward extension point (min-version,
signing) without schema churn on manifests.

**Name precedence:** `--name` flag > index `name`. Basename inference is
dropped for dir/git sources (breaking; migration = write the index).
`--openapi` sources are exempt — no index required, anyctl synthesizes the
manifest and metadata (name from `--name`/`info.title` as today).

**Install shape:** index fields fold into the existing `.anyctl-catalog.json`
provenance record (`description`, `version`, `homepage` added to
`CatalogMeta`). The raw index file is NOT copied into `catalogs/<name>/`, so
the loader (`internal/manifest/load.go`) needs no change beyond a defensive
filename exclusion. `catalog validate` gains the same index checks (present,
schema-valid, members exist + portable, dup-name check).

**Schema:** new `schema/catalog.schema.json` mirroring
`schema/manifest.schema.json`; `anyctl schema` grows an optional arg —
`anyctl schema [manifest|catalog]`, default `manifest` (back-compat).

## 2. Git as the first-class marketplace (forge-agnostic)

No new fetch machinery — `catalog add <git-url> [--ref --path]` already clones
via system git (schemes allow-listed, `ext`/`fd` blocked, URL after `--`,
SHA-pinned in metadata) and `catalog update` re-resolves the recorded ref to a
new pin. This pass makes git the *documented default distribution path*:

- README "Catalogs" section rewritten: publish = push a repo containing
  `anyctl-catalog.yaml` + manifests; install = `anyctl catalog add <url>`;
  works on GitHub, Forgejo, Codeberg, GitLab, any git host. CI = the existing
  `validate-catalog` composite action (unchanged mechanically; docs refreshed).
- `examples/catalog/` gains the index file (reference catalog = reference
  index). `examples/full/` unaffected (it is the at-rest installed form).
- No `--file` flag to rename the index: one well-known filename IS the
  discovery contract; `--path` already handles location within a repo.

## 3. Private catalogs: SSH now, https/PAT deferred to an issue

SSH is the blessed private-repo path, documented — it already works:
`git@host:path` / `ssh://` pass `validateGitURL` and defer auth to system git
(`~/.ssh/config`, ssh-agent). New README subsection covers: per-forge key setup
(GitHub deploy keys / user keys; Forgejo/Codeberg/GitLab equivalents), that
`GIT_TERMINAL_PROMPT=0` means a passphrase-protected key needs an agent (fails
closed, never hangs), and the "Permission denied (publickey)" triage.

**Deferred (issue, not built):** open a GitHub issue on jedwards1230/anyctl
proposing https/PAT auth: per-host token *refs* in `config.yaml`
(`catalogs.git_auth.<host>.token: op://…`) resolved through the existing secret
provider registry and injected into the git subprocess via a one-shot
`GIT_ASKPASS`/credential-helper env — forge-agnostic PATs, never argv, never
logged, mirroring the op provider's token-injection pattern. Issue linked from
the PR.

## 4. CLI surface — before/after

| Before | After | Why |
|---|---|---|
| `catalog installed` | **`catalog list`** | the standard verb (`helm repo list`); "installed" is an adjective, not a command. Old name dropped, no alias (pre-1.0). |
| — | **`catalog info <name>`** (new) | one catalog's identity + pin: description, version, homepage, source, ref/commit, services. The marketplace-browsing verb. |
| `catalog installed` output: `NAME TYPE VER SOURCE` | `catalog list`: `NAME  VERSION  SERVICES  PINNED  SOURCE` | index version + service count; commit/ref folded into PINNED; description lives in `info`. |
| `catalog add <src>` accepts any dir/repo of `*.yaml` | requires `anyctl-catalog.yaml` at the root | discovery contract (§1); error names the file and shows a 3-line starter. |
| add name default: dir/repo basename | `--name` > index `name` | author-controlled identity; no more `cat.git` → `cat` surprises. |
| `catalog update [name]` | `catalog update [name...]` | update several at once; per-catalog non-abort semantics already exist. |
| `catalog add --openapi` (bool flag) | unchanged | still the odd flag out, but harmless and documented; not worth a subcommand. |
| `--name --ref --path --force` | unchanged | already clear and conventional. |
| `catalog validate <dir>` | unchanged name; now validates the index too | same gate as `add`, as today. |
| `catalog remove <name>` | unchanged | |
| `anyctl schema` | `anyctl schema [manifest\|catalog]` (default `manifest`) | additive; editor validation for index files. |

Plus a full help/description pass over the `catalog` tree: consistent
imperative shorts, every Long mentions the index file where relevant, error
messages actionable (missing index → print the starter snippet).

**Breaking changes (pre-1.0, allowed):**
1. dir/git `catalog add`/`validate` fail without `anyctl-catalog.yaml`.
2. `catalog installed` → `catalog list`.
3. Basename name inference dropped for dir/git sources.

## 5. Migration & coordination

- `examples/catalog/`: index added in this PR; `examples/full` untouched.
- **Private `orchestration/anyctl-catalog/` (cross-repo follow-up, not built
  here):** needs its `anyctl-catalog.yaml` added — and it must land BEFORE the
  devcontainer/Ansible hosts upgrade to the anyctl release containing this
  change, or their `catalog add`/`update` breaks. One-file PR in
  home-orchestration; called out in the anyctl PR body.
- `.github/actions/validate-catalog`: no mechanical change; docs refreshed.

## 6. Implementation order

1. `internal/manifest`: index type + parse/validate (+ `schema/catalog.schema.json`),
   `CatalogMeta` fields, member selection in the shared `validateEntries` walk.
2. `internal/cli`: add-gate wiring, name precedence, `installed`→`list`,
   new `info`, `update` variadic, `schema` arg, help-text pass.
3. Docs: README / CLAUDE.md / CONTRIBUTING.md / examples in the same change.
4. Open the https/PAT issue; PR references it.
5. Gates: gofmt, go vet, golangci-lint 0 issues, `go test -race` ≥75%,
   `go build`, `go mod tidy` no-op. PR labeled `semver:minor` with a
   "Breaking CLI changes" section.
