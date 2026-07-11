# CLAUDE.md

@CONTRIBUTING.md

Guidance for Claude Code in this repository.

## What this is

`anyctl` is a single, manifest-driven Go CLI for HTTP/RPC service APIs. A service
is one `services/<name>.yaml` manifest; the binary compiles in **zero**
service-specific logic. Adding/removing a service is a YAML edit, never a
recompile. It replaces a set of bespoke per-service bash wrappers.

**Design rationale.** A *manifest* is the portable description of what a service
*is* — its commands, auth strategy, and secret slots — identical for every user.
A *profile* is the per-machine binding — the `base_url`, secret refs, and any
endpoint overrides that make a manifest usable *here*. The binary is an
**unopinionated executor**: it resolves a manifest against a profile and runs
exactly the request described, gating nothing except a step a manifest itself
marks `confirm:`. That split (portable manifest / per-machine profile / neutral
executor) is the whole model — everything below elaborates it.

## Core principle: unopinionated executor

The binary **gates nothing** — no `--read-only`, no MCP write-gating — **except** a
step a manifest explicitly marks `confirm:`, which aborts unless `--yes/-y` clears
it (manifest-opt-in, fail-closed; no interactive prompt). It otherwise does exactly
what the manifest says. Guardrails belong in the consuming layer (an agent-host
pre-call hook), not baked into the tool. Don't add safety/policy logic here.

## Architecture

```
main.go                 entry → internal/cli
internal/
  manifest/   YAML model + XDG load/merge + schema validation + installed-catalog merge + catalog store
  command/    format-neutral Command model + producers (commands: block, generic verbs)
  template/   {secret.X}/{env.X}/{arg.N}/{var} expansion (JSON braces pass through)
  secret/     scheme-dispatched Provider interface (op:// → 1Password) + env override
              + idioms + cache; op provider injects OP_SERVICE_ACCOUNT_TOKEN into
              its subprocess only (never argv/log)
  auth/       apply none/header-key/bearer/basic/oauth2-client-credentials/ws-login to a request
  transport/  http (curl-equivalent) + jsonrpc-ws; error extraction, typed errors→exit codes
  output/     gojq filter + render modes (json/raw/scalar)
  engine/     resolve template→endpoint→auth→transport; pagination (none/fixed-query/cursor/page-number/page-until-short)
  agentsafety/ shared agent-safety layer: secret scrubber, dry-run preview render, exit-code taxonomy+classifier, tool-annotation policy, mutation audit log (renders/classifies/records — gates nothing)
  telemetry/  optional OpenTelemetry tracing (no-op unless OTEL_* env configures it)
  cli/        cobra tree, dynamic per-service registration, builtins, exit-code mapping
```

**Telemetry**: off by default; one span per invocation when `OTEL_*` is set.
Fail-open, time-bounded flush — never blocks a command. The CLI emits one span
per invocation; the now-shipped MCP server reuses the same provider and emits
one span per tool call (`<svc>_<command>`). Metrics remain future work.

**Two faces, one executor**: the CLI and the MCP server (stdio or
streamable-HTTP) both drive `engine.Execute`, so behavior is identical. On the
MCP face every tool takes an optional `dry_run` (previews, no network), error
results are structured (`IsError` + text fallback + `StructuredContent
{error,class,status}`), and each WRITE call emits one JSON mutation audit record
to stderr. It still gates nothing.

## Capabilities (all shipped)

- **Transports**: `http` (curl-equivalent) + `jsonrpc-ws` (ws-login; truenas,
  sunshine execute fully).
- **Auth**: none / header-key / bearer / basic / oauth2-client-credentials
  (on-disk token cache) / ws-login.
- **Secrets**: scheme-dispatched providers (`op://` → 1Password, optional
  service-account-token injected into the `op` subprocess only) + `<PREFIX>_<SECRET>`
  env override. Adding a backend is three edits in `internal/secret/provider.go`
  (new `Provider`, config block, `NewRegistry` case) — no engine/cli changes.
- **Commands**: hand-written `commands:` or OpenAPI inference (`spec:` +
  `spec_filter:` via libopenapi); composed `steps:` pipelines
  (extract/when/confirm/on_error); gojq output.
- **Builtins**: `list`/`lint`/`doctor`/`schema`/`init`/`self-update` (sha256-verified
  in-place update from the GitHub release).
- **MCP server** (`anyctl mcp`, stdio or `--http :9000` with `/mcp` + `GET /healthz`):
  a non-loopback `--http` bind refuses to start without a bearer token
  (`ANYCTL_MCP_AUTH_TOKEN` / `--auth-token-file`) unless `--allow-unauthenticated`;
  loopback needs none.

## Catalogs

anyctl ships **no built-in services** — there is no embedded floor. Two sources,
highest wins: local `services/<name>.yaml` > installed catalog (`catalogs/*/`).
Absent any local `services/` or installed `catalogs/`, a config resolves to ZERO
services and `list`/bare `svc` print an actionable hint (exit 0) while `svc
<name>`/`doctor`/`mcp` error (exit 2). A manifest is plain YAML, so editing is
**rebuild-free**. To author a NEW manifest, scaffold with `anyctl init` into
`services/` and validate with `anyctl lint`.

**Git is the first-class distribution path.** Every dir/git catalog source MUST
carry an `anyctl-catalog.yaml` index at its root (the `--path` subdir for a subdir
install) — the required identity record: `name` + `description` (both required),
optional `version`/`homepage`, and an optional curated `manifests:` member list
(absent → auto-glob top-level `*.yaml` except the index; present → exactly those,
in order, fail-closed). The index is NOT copied into `catalogs/<name>/`; its
fields fold into `.anyctl-catalog.json` (`CatalogMeta.Description/Version/Homepage`).
Model + parse/validate: `internal/manifest/catalogindex.go`; schema:
`schema/catalog.schema.json` (embedded as `schema.Catalog`, printed by `anyctl
schema catalog`). `--openapi` sources are index-exempt (anyctl synthesizes the
manifest). `catalog` manages INSTALLED (named) catalogs only:

- `catalog add <source> [--name --ref --path --force]` — install a dir or git repo
  of portable manifests. Reads the index, then validates every member `*.yaml`
  (schema + portability) fail-closed, installs atomically. Name = `--name` > index
  `name` (no basename inference). Git pinned to its commit SHA in
  `.anyctl-catalog.json`; fetches shell to system `git` with `ext`/`fd` blocked,
  URL after `--`. `--path <subdir>` (git-only) installs from a subdirectory of the
  repo (recorded in the metadata so `update` re-fetches the same subdir; rejects
  absolute/`..` traversal).
  `--openapi <url|file>` materializes one manifest from an OpenAPI 3.x doc
  (operations → `commands:`, `securitySchemes` best-effort → `auth:`, un-mappable
  → `strategy: none`; spec parsed once, not vendored).
  Impl: `internal/manifest/openapi_scaffold.go`, `internal/cli/catalog_openapi.go`.
- `catalog update [name...]` (variadic; re-reads each source's index) / `remove
  <name>` / `list` (NAME VERSION SERVICES PINNED SOURCE) / `info <name>` (one
  catalog's identity, provenance, and services).
- `catalog validate <dir>` — the SAME fail-closed gate `catalog add` runs (index
  present + valid, then `ValidatePortableManifest` + duplicate-name check),
  read-only and config-dir-free (per-file `ok`/`FAIL`, exit 0/2). What a
  third-party catalog repo runs in CI. Impl: `internal/cli/catalog_validate.go`.

**Two installed catalogs MAY share a service name** — both install; each stays
addressable as `<catalog>:<service>`. The bare name becomes ambiguous:
`Loaded.Lookup` returns a `*ConfigError` (exit 2) listing both qualified forms
(never silently picks). The MCP server names a tool from the *selector*, so a
colliding install **renames** the first catalog's tools to
`<catalog>-<service>_<command>` (`internal/mcpserver/mcpserver.go`'s
`selectorToolPrefix`) — inherent to disambiguation. Store API:
`internal/manifest/catalogstore.go`; add-gate: `internal/manifest/schemacheck.go`;
CLI: `internal/cli/catalog_install.go`. `.github/actions/validate-catalog` is the
composite action third-party repos point CI at; `examples/catalog/` (singular) is
the reference catalog.

## MCP Apps result View (read tools only)

Every read tool (`!Write`, incl. the generic `<svc>_get`) carries
`_meta.ui.resourceUri = "ui://<FederationName>/result"` — one universal
table/record/tree HTML View registered ONCE (`internal/mcpserver.BuildServer`),
zero per-service Go. The View is a single built HTML file
(`internal/mcpserver/views/result.html`, built from the `views/` TS/Vite project
and committed so plain `go build` needs no npm) `//go:embed`'d, with
`ANYCTL_VIEWS_DIR` overriding from disk for the dev loop. `executeAndRender`
populates `CallToolResult.StructuredContent` ADDITIVELY (the `TextContent`
fallback is unchanged): `{"result": <jq-filtered value>, "<FederationName>":
{"service","command","title","ui"}}` (the wrapper key is `brand.FederationName`).
Write tools and dry-run get neither the link
nor StructuredContent. An optional per-command `ui:` hint block (`manifest.UI`,
sibling of `output:` — `view`/`columns`/`primary`/`badges`/`sort`/`drilldown`) is
DATA only (no HTML/URLs/secrets), so it stays portable; absent, the View
auto-detects by shape. A write-confirmation View is a later PR.

> **The MCP wire strings are a pinned constant (`brand.FederationName`).** The
> MCP server name, the result-View resource URI, and the StructuredContent
> wrapper key all read from it; the ContextForge gateway federates under that
> exact string. Don't change it without updating the gateway in lockstep — it's
> self-documented at its definition in `internal/brand/brand.go`.

## Conventions

- stdout = data, stderr = diagnostics, real exit codes (0 ok, 2 usage, 3 auth,
  4 HTTP≥400, 5 network, 6 decode).
- Secrets are refs (`op://...`) resolved at call time — never values in manifests,
  never in argv, redacted in verbose/dry-run output.
- Services resolve from **two sources, highest wins**: a local
  `<config-dir>/services/<name>.yaml` > an installed named catalog
  (`<config-dir>/catalogs/*/`). There is no built-in floor. `list` marks each
  `local`, `override` (a local file shadowing an installed catalog), or
  `catalog:<name>` (from an installed catalog). Two *local* files with one name is
  still a duplicate error. Two *installed catalogs* defining one name is **not**
  an error — both stay addressable as `<catalog>:<service>`; the bare name is
  ambiguous and errors (listing both qualified forms) until you qualify it.
  Absent any local `services/` or `catalogs/`, a config has ZERO services and
  `list`/bare `svc` print an actionable hint (exit 0) while `svc <name>`/`doctor`/
  `mcp` fail with the same guidance (exit 2).
- A manifest is **portable** (what a service *is*); user-specific endpoints and
  credentials (`base_url`, secret `ref`s, per-machine endpoint/var/tls overrides)
  live in a `profile.yaml` at the config root, which is the **sole** binding
  mechanism. Precedence is **env override > profile**. A manifest may **not**
  carry a `base_url` (service or endpoint) or a secret `ref` — structural
  `Validate` rejects it (`*ConfigError` → exit 2, message points at the
  `profile.yaml` slot); an in-manifest secret `env:` stays allowed (a
  CI/devcontainer override). Structural `Validate` (well-formed, runs on the RAW
  pre-merge manifest) is split from `ValidateComplete` (post-merge: resolvable
  base_url + every secret bound); completeness is enforced post-merge at execute
  time and surfaced by `doctor` / `lint --strict`. Portable + `profile.yaml` is
  the form the shipped `examples/` use.
- New auth strategy / transport / pagination style → wire it in its package + add
  a test; keep the manifest schema additive.
- Release: opt-in `semver:*` label on the merged PR (no label → no release);
  shared `ai-release.yml@v1`; ships cross-compiled static binaries.
