# anyctl — Product Requirements

This is the single requirements source of truth for `anyctl`. It describes current state
(verified against the source on 2026-10-11), the scope decisions that bound it, and what
counts as success. Where an artifact enforces a rule (a schema, a named test, a code
table), this doc cites it and does not restate it. If prose here disagrees with one of
those artifacts, the artifact wins and this doc is the bug.

## 1. Purpose and users

`anyctl` is one static Go binary that turns a YAML **manifest** (what an HTTP/RPC service
*is*) plus a per-machine **profile** (where it lives, which credentials) into a shell CLI
and an MCP tool server. The binary contains no service-specific code.

| User | Interface | Core jobs |
|---|---|---|
| Human at a shell | `anyctl svc <service> <command>` | Call a service API without writing curl/jq/auth/pagination wrappers. Preview with `--dry-run`. Script it, using exit codes and stdout. |
| AI agent | `anyctl mcp` (stdio or streamable-HTTP) | Discover per-command tools, preview a call (`dry_run`), get structured results and typed errors, without a bespoke MCP server per service. |
| Catalog author (including third parties) | `anyctl init`, `anyctl lint`, `anyctl catalog validate`, the `validate-catalog` action | Publish portable manifests in a git repo that anyone can install. The repo carries no endpoints and no credentials. |
| Catalog consumer / operator | `anyctl catalog add/update`, `profile.yaml`, the Docker image and Helm chart | Install shared catalogs, bind them to local hosts and secrets, and run the MCP server in-cluster. |

Scenarios the product must serve:
- A new user runs `examples/quickstart` (no LAN, no secrets) and gets a live response.
- An operator adds a service by writing YAML, with no rebuild and no restart of the CLI.
  Manifests are re-read on every invocation.
- An agent federates `anyctl mcp --http` behind a gateway and calls read tools with a View.
  Write tools are exposed only when the operator chooses to expose them.
- A third party publishes a catalog. A consumer installs it, and it stays inert until
  their profile binds it.

## 2. Principles and invariants

Breaking one of these is a defect even if every test passes.

| # | Invariant | Where it holds |
|---|---|---|
| P1 | **Unopinionated executor.** The binary runs exactly the request the manifest describes. The only execution gate is a manifest step marked `confirm:`, which aborts unless `--yes/-y` is passed (no prompt). Policy belongs to the consuming layer (agent-host hooks, gateways). `mcp --read-only` and `--service` are operator-chosen *exposure filters* applied when the server starts, not per-call policy. | `internal/engine/pipeline.go` (confirm check); `TestPipelineConfirmDefaultDeny`, `TestPipelineConfirmYesProceeds`; `internal/mcpserver/mcpserver.go` `Options` |
| P2 | **Manifest/profile split is a trust boundary.** A manifest never carries a service `base_url`, an endpoint `base_url`, or a secret `ref`. Binding happens only in `profile.yaml` or through env overrides (env beats profile). An in-manifest secret `env:` is allowed. This keeps a shared catalog inert. | `validateNoInManifestBinding` in `internal/manifest/validate.go`; `TestValidatePortableManifestRejectsBindings`, `TestCatalogValidateRejectsBinding` |
| P3 | **Unix-native.** stdout carries data, stderr carries diagnostics, and exit codes are typed. Secrets never appear in argv and are redacted from verbose, dry-run and error output. | Exit taxonomy: `internal/agentsafety/exit.go` (`Classify`, `ClassName`), `TestClassifyExitCodes`; redaction: `internal/agentsafety/scrub.go`, `TestScrubberInURLAndJSON` |
| P4 | **The template grammar is not a programming language.** It does one level of `{token}` substitution, and an unknown token is an error. jq (gojq) is the only data grammar. | `internal/template/template.go` package doc; `internal/template/template_test.go` |
| P5 | **No built-in services.** With no local `services/` and no installed catalog, a config resolves to zero services. `list` and bare `svc` print a hint and exit 0; `svc <name>`, `doctor` and `mcp` exit 2. | `noServicesHint` in `internal/cli/builtins.go`; `internal/manifest/load.go` |
| P6 | **The schema is additive and closed.** Every object is `additionalProperties: false` except `annotations:`. New fields are additive, and catalog ingest fails closed. The schema and the structural validator must agree. | `schema/manifest.schema.json`, `schema/catalog.schema.json`; `TestSchemaAndValidateAgree`, `TestCatalogSchemaAndParseAgree`, `TestKnownInvalidManifestsFailSchema` |
| P7 | **One executor, two faces.** The CLI and MCP both drive `engine.Execute`, so behavior differs only where §4 says it does. | `internal/cli/cli.go` `dispatch`; `internal/mcpserver/mcpserver.go` `executeAndRender` |

The MCP wire identity (server name, View URI, StructuredContent key) is the pinned constant
`brand.FederationName` (`internal/brand/brand.go`). Changing it requires updating the
federating gateway in the same change.

## 3. Current feature inventory

### 3.1 CLI commands (`internal/cli/`)

| Command | Behavior | Source |
|---|---|---|
| `list` | Prints each service with its origin: `local`, `override` or `catalog:<name>`. | `builtins.go` `cmdList` |
| `svc` / `s` | Bare: same output as `list`. `svc <selector> <command> [args…]` runs a named command. `<selector>` is either a bare name or `<catalog>:<service>`. | `cli.go` `newSvcCmd` |
| Generic verbs | `svc <s> get\|post\|put\|patch\|delete\|head <path> [body\|query]`. `call <method> [json-params]` exists on `jsonrpc-ws` services. A named command with the same id takes precedence over the verb. `@file` body works. | `internal/command/verb.go`, `cli.go` |
| `doctor [service]` | Unauthenticated GET of `base_url`, or `incomplete: …`. Skips ws and templated bases. | `builtins.go` `probe` |
| `lint [service\|file.yaml] [--strict]` | Runs structural `Validate`. `--strict` adds `ValidateComplete` (base_url and every secret bound, checked after the profile merge) and reports every service. | `builtins.go` `cmdLint` |
| `init` | Bare: idempotently creates `config.yaml`, `services/` and `profile.yaml`. `init <name> [--auth …] [-o file] [--force]`: writes a portable starter manifest. | `init.go`, `internal/manifest/scaffold.go` |
| `schema [manifest\|catalog]` | Prints the embedded JSON Schema. | `schema.go`, `schema/schema.go` |
| `catalog add\|update\|remove\|list\|info\|validate` | See §3.9. There is **no** `catalog edit`. | `catalog.go` |
| `mcp` | See §3.10. | `builtins.go` `cmdMCP` |
| `self-update [--check] [--version vX.Y.Z] [--force]` | Downloads `anyctl-{os}-{arch}` from the GitHub release, checks it against the sibling `.sha256`, and replaces the binary atomically. | `selfupdate.go` |
| `version` | Prints the version: the ldflags value, else the `go install` module version, else `dev`. | `builtins.go`, `main.go` |

Global flags (`cli.go` `newRoot`): `--config-dir`, `--filter`, `--raw`, `--query`,
`--limit` (appends `limit=N`), `-o/--output json|raw|scalar`, `--endpoint`, `--dry-run`,
`-v/--verbose`, `--json-errors` (JSON envelope `{error,service,command,status,detail}` on
stderr, `errors.go`), `-y/--yes`.

### 3.2 Config layout and resolution

| Path | Role | Decode |
|---|---|---|
| `$ANYCTL_CONFIG_DIR` > `--config-dir` peek > `$XDG_CONFIG_HOME/anyctl` > `~/.config/anyctl` | Config root (`ConfigDir`, `load.go`) | — |
| `config.yaml` | Defaults (`timeout` 60s, `output` json, `max_response_bytes` 64 MiB) and secrets providers | strict (`KnownFields`) |
| `profile.yaml` | Per-service `base_url`, `tls_insecure`, `vars`, `endpoints.<n>.{base_url,tls_insecure}`, `secrets.<n>.{ref,env}`. Per-key merge over the manifest. Binding an unknown service produces a warning. | strict (`profile.go`) |
| `services/*.yaml` | Local manifests | **lenient** (`yaml.Unmarshal`, see §4) |
| `catalogs/<name>/*.yaml` + `.anyctl-catalog.json` | Installed catalogs. Dot-dirs are skipped. | lenient |

Precedence when two sources define the same service: a local file beats an installed
catalog (origin `override`). Two local files with the same name is an error. Two catalogs
with the same name both load; the bare name becomes ambiguous (exit 2 with both qualified
forms), and each catalog's copy stays reachable as `<catalog>:<service>`. A local file
**replaces** the catalog manifest whole; there is no merge. Env overrides: `<PREFIX>_URL`
(default endpoint only), `<PREFIX>_<VAR>` for vars, and `<PREFIX>_<SECRET>` or the
secret's `env:` for secrets.

### 3.3 Manifest features

Field list and enums: `schema/manifest.schema.json`. Go model:
`internal/manifest/types.go`.

| Feature | Current behavior | Source |
|---|---|---|
| `transport` | `http` (default) and `jsonrpc-ws` (WebSocket JSON-RPC with connection-scoped `ws-login`). | `internal/transport/` |
| `auth.strategy` | See the auth table below. | `internal/auth/` |
| `secrets` | Named slots with optional `env`, `fields` (ordered fallback), and `idiom` `read\|item-get\|item-json` (1Password). | `internal/secret/` |
| `vars` | Template vars, overridable by env and by profile. | `engine.go` `envOverrideVars` |
| `endpoints` | Named targets with their own `auth` (including `none`), `tls_insecure` and `codec`. Selected by a command's `endpoint:` or by `--endpoint`. | `engine.go` `resolveEndpoint` |
| `codec` | Request `json\|form` (sets Content-Type). Response `xml` is decoded to a jq tree (`@attrs`, `text`). | `internal/output/render.go` `DecodeXML` |
| `pagination` | `none`, `fixed-query`, `cursor` (`next` jq path, cycle guard), `page-number`, `page-until-short`. Pages are merged into `{<data-key>: [...]}`. Hard cap of 1000 pages. `--limit` truncates. | `engine.go` |
| `output` | `default_filter`/`filter` (gojq) and `mode` `json\|raw\|scalar`. Precedence: flag > command > service > `config.defaults.output`. | `internal/command/command.go`, `render.go` |
| `ui` | Data-only View hints: `view`, `columns`, `primary`, `badges` (`bool`), `sort`, `drilldown`. | `validate.go` `validateUI` |
| `path_rules.trailing_slash: before-query` | Appends `/` to the path before the query string (non-pipeline commands). | `engine.go` |
| `spec` / `spec_filter` | OpenAPI 3.x (libopenapi) → inferred commands. Explicit `commands:` win. Accepts an http(s) URL (cached 24h at `$XDG_CACHE_HOME/anyctl/specs`) or a file path relative to the config dir. A failure falls back to static commands with a warning. | `internal/manifest/spec.go` |
| `tls_insecure` | Service level, endpoint level, or set by the profile. | `engine.go` |
| `noauth`, `headers`, `query`, `body` (`@file` or inline), `params` (JSON array, template-tolerant) | Per command. | `engine.go`, `validate.go` |
| `mcp_ignore` | Hides the command from MCP. The CLI still runs it. | `mcpserver.go` |
| `annotations` | Free-form map, top level and per command. Never validated or read. | `types.go` |

| Auth strategy | Behavior |
|---|---|
| `none` / empty | No credential. |
| `header-key` | `header: value` (templated). |
| `bearer` | `Authorization: <scheme or Bearer> <value>`. |
| `basic` | `username`/`password`. |
| `oauth2-client-credentials` | `token_url`, `client_id`, `client_secret` (legacy `value`/`username`/`password` still work). Token is cached on disk in 0600 files. `internal/auth/oauth2.go`. |
| `ws-login` | jsonrpc `method` + templated `params`, sent once per connection. |
| `login-flow`, `external-tool` | Accepted by the schema and by `Validate`, but **fail at call time** with "not yet implemented". `auth.go`. |

### 3.4 Secrets (`internal/secret/`)

Resolution order per secret: the secret's `env:` var, then `<PREFIX>_<SECRET>` (when
`env_override` is on or `env:` is set), then the provider for the ref's URI scheme.
Values are cached per invocation (shared across pipeline steps) and never written to
disk. A provider failure classifies as auth (exit 3). A missing provider or an undeclared
secret classifies as usage (exit 2).

| Provider | Behavior |
|---|---|
| `onepassword` (`op://`) | Runs `op read {ref}` by default, or `op item get` for the item idioms. An optional `service_account_token` (exactly one of `file\|value\|env`; a token file must be 0600/0400) is injected as `OP_SERVICE_ACCOUNT_TOKEN` into the subprocess only. `--dry-run` prints the resolved `op` path. |
| `exec` (`pass://`, `vault://`, any scheme) | Runs a `{ref}`-templated `command` and returns its stdout; `read` idiom only. A `command` is required. `exec.go`. |
| `file` (`file://`) | Reads the value from an owner-only (0600/0400) file; `read` idiom only. `file.go`. |
| Kind selection | `type: onepassword\|exec\|file`, inferred from the scheme when omitted (`op` → onepassword, `file` → file, else exec). `manifest.ProviderKinds` is the one list both `ValidateConfig` and `secret.NewRegistry` use; each scheme may have one provider, and `service_account_token` is onepassword-only. `TestValidateConfig`, `TestRegistryBuildsEveryProviderKind`. |
| Legacy `secret:` block | Permanent alias, normalized into an op provider (`NormalizeSecrets`). |

### 3.5 Template grammar (`internal/template/template.go`)

Tokens: `{secret.X}`, `{env.X}` (unset → `""`), `{argN}` / `{arg.N}` (positional), and
`{name}` (a service var, or in pipelines an extracted or captured var). Braces that don't
match a token (JSON) pass through literally. There is no escaping, no defaults, no
conditionals and no filters. Template expansion covers path, query, headers, body,
`params`, auth values and `base_url`. The jq fields (`extract`, `when`,
`body_transform`, `output.filter`) are not templates.

### 3.6 Multi-step `steps` pipelines (`internal/engine/pipeline.go`)

A command with a non-empty `steps:` runs its steps **sequentially within one service**.
Behavior is pinned by `internal/engine/pipeline_test.go` (`TestPipelineMultiStepExtract`,
`…CaptureHeader`, `…WhenSkip`, `…OnErrorContinue`, `…ConfirmDefaultDeny`,
`…BodyTransform`, `…FinalFilterAssembly`, `…DryRun`).

| Step field | Behavior |
|---|---|
| `path`, `query`, `headers`, `body`, `endpoint`, `method` | Templated against vars, args and earlier extracts. `method` defaults to GET. A named endpoint brings its own auth, TLS setting and codec. |
| `extract: {var: jq}` | Runs jq against the decoded response and stores the result as a var. Objects are JSON-encoded when templated. |
| `capture_header: {var: Header}` | Stores a response header value as a var. |
| `decode: xml` | Decodes the response as XML before `extract`. The endpoint's response codec is the fallback. |
| `when: jq` | Evaluated against the accumulated vars. If falsy, the step is skipped. Runs **before** `confirm`. |
| `confirm: msg` | Aborts unless `--yes`. |
| `on_error: <step>` | Runs a fallback step, then continues. If the fallback's endpoint can't resolve, the original error is surfaced. |
| `body_transform: jq` | Builds the JSON body from the accumulated vars and replaces `body`. |
| Final `output.filter` | jq over the accumulated var map (vars plus `env`), not over the last response. |

There is no pipeline example in `examples/`. A real single-service example (sunshine
`status`, radarr `add`) lives **outside this repo**, in
`jedwards1230/home-orchestration`'s `anyctl-catalog/`.

### 3.7 Validation tiers

| Tier | Runs on | Checks |
|---|---|---|
| Structural `Validate` | Every load and `lint` | No in-manifest binding (P2), transport, strategy, required auth fields, `{secret.X}` declared, idiom, output mode, pagination style, endpoint references, step targets, jq compiles, ws `params` is an array, `ui` enums |
| `ValidateComplete` | Execute (including dry-run), `doctor`, `lint --strict` | `base_url` or endpoints bound, every declared secret bound |
| JSON Schema (`SchemaValidate`) | **Only** `catalog add`, `catalog validate`, `catalog add --openapi` (`ValidatePortableManifest`) | Closed schema: unknown keys fail |

### 3.8 Output and errors

gojq is used everywhere. `json` mode is pretty-printed with HTML escaping off, matching jq.
`scalar` prints bare strings. `raw` bypasses the filter. A response body is bounded by
`max_response_bytes`. HTTP errors carry an extracted `detail`.

### 3.9 Catalogs

| Capability | Behavior | Source |
|---|---|---|
| Index | `anyctl-catalog.yaml` is required: `name`, `description`, and optional `version`, `homepage` and `manifests` (curated list; when absent, top-level `*.yaml` is globbed). Strict decode. | `schema/catalog.schema.json`, `internal/manifest/catalogindex.go` |
| `catalog add <src>` | Takes a local dir or a git URL (https/http/ssh/git/file). `--ref`, `--path <subdir>` (git only, no traversal), `--name` (otherwise the index `name`), `--force`. Validates all manifests fail-closed, then installs atomically. The git commit SHA is pinned in `.anyctl-catalog.json`. Git runs with `GIT_TERMINAL_PROMPT=0`, `ext`/`fd` blocked, and the URL after `--`. | `internal/cli/catalog_install.go`, `internal/manifest/catalogstore.go` |
| `catalog add --openapi <url\|file>` | Synthesizes one manifest from OpenAPI 3.x (no index needed). | `internal/manifest/openapi_scaffold.go` |
| `update [name…]`, `remove`, `list`, `info` | `update` re-fetches from the recorded source, ref and path. | `catalog_install.go`, `catalog_info.go` |
| `catalog validate <dir>` | Read-only, needs no config dir. Same gate as `add`. Prints `ok`/`FAIL` per file and exits 0 or 2. | `catalog_validate.go`, `TestCatalogValidate*` |
| CI action | `.github/actions/validate-catalog` (`go install` then `catalog validate`). | `action.yml` |
| Reference catalog | `examples/catalog/`, checked by `TestExampleCatalogValidates`. | |

### 3.10 MCP server (`internal/mcpserver/`)

| Capability | Behavior |
|---|---|
| Tools | One tool per non-`mcp_ignore` command, named `<prefix>_<command>`. The prefix is the selector with `:`→`-`, so only a cross-catalog collision renames tools. Plus generic `<prefix>_get/_post/_put/_patch/_delete` (HTTP; no HEAD) or `<prefix>_call` (ws). |
| Input schema | Named commands: `arg0..argN` (strings; N is the highest `{argN}` across path, query, body, params and step path/query/body), `filter`, `raw`, `dry_run`, with `required: []`. Verbs: named `path`/`query`/`body` or `method`/`params`, with `path`/`method` required. |
| Scoping | `--read-only` omits tools where `Write` is true. `--service a,b` is an allowlist and accepts qualified names. Unknown names exit 2. |
| Write classification | HTTP: any method other than GET/HEAD/OPTIONS/empty. jsonrpc: anything not matching `.query/.get/.config/core.ping/.list/.info`. `call` is always write. `command.go` `isWrite`. |
| Annotations | `readOnlyHint`; DELETE/PUT get `destructive` + `idempotent`; POST/PATCH get non-destructive; `openWorldHint` is always true; descriptions carry a `[WRITE]` suffix. `agentsafety.Hints`, `TestHints`. |
| Dry-run | Builds the request with no network and no secret resolution; auth is shown as `<redacted>`. `TestDryRunToolInputSkipsNetwork`. |
| Errors | `IsError` + text + `StructuredContent {error, class, status?}`. `TestStructuredTypedErrorResult`. |
| Audit | One JSON line on stderr per **write** call (ok/error/dry-run). `{secret.X}` tokens are masked; `caller` is `"unknown"`. `TestMutationAuditRecordEmittedOnWrite`. |
| Secret scrubbing | Resolved values are scrubbed from verbose output and error strings (`agentsafety.NewScrubber`). Response bodies are returned unmodified. |
| Result View | Read tools get `_meta.ui.resourceUri = ui://<FederationName>/result` and `StructuredContent {result, <FederationName>: {service, command, title, ui}}`. One embedded HTML file (`views/result.html`; `ANYCTL_VIEWS_DIR` overrides). Write tools get neither. A dry-run result carries no StructuredContent. |
| Transports | stdio (default). `--http addr` serves `/mcp` and `GET /healthz`. A non-loopback bind refuses to start without `ANYCTL_MCP_AUTH_TOKEN` / `--auth-token-file` (constant-time bearer) unless `--allow-unauthenticated` is passed. |

### 3.11 Distribution and telemetry

| Item | Fact | Source |
|---|---|---|
| Releases | One immutable `vX.Y.Z` per merged PR, by label rule. Static `CGO_ENABLED=0` binaries for linux/darwin × amd64/arm64, each with a `.sha256`. | `.github/workflows/release.yml`, `.github/.releaseignore` |
| Signing | Aggregate `SHA256SUMS` is cosign keyless-signed (`SHA256SUMS.sigstore.json`). | `release.yml` |
| Image | `ghcr.io/jedwards1230/anyctl:{version,latest}`, multi-arch, bundles `op`, non-root, default `mcp --http :9000`. | `Dockerfile` |
| Chart | `oci://ghcr.io/jedwards1230/charts/anyctl-mcp`. Config/profile/services from ConfigMaps; bearer auth via existingSecret or OnePasswordItem; opt-in NetworkPolicy. | `deploy/helm/anyctl-mcp/` |
| `go install` | `github.com/jedwards1230/anyctl@vX.Y.Z`. | `main.go` `resolveVersion` |
| Telemetry | Off unless `OTEL_EXPORTER_OTLP_(TRACES_)ENDPOINT` is set. One span per CLI invocation (`<svc> <cmd>`) or per MCP tool call (`<svc>_<cmd>`). Pipeline steps are span events. http/protobuf by default, grpc optional, 2s bounded flush, fail-open. No metrics. | `internal/telemetry/telemetry.go` |

## 4. Known limitations and gaps

Current-state facts, verified in source.

| # | Gap | Evidence |
|---|---|---|
| L1 | MCP inputs are untyped `arg0..N` strings and none are required. On MCP a missing arg becomes `""` and the request still runs; on the CLI the same template errors ("only K args were given"). | `mcpserver.go` `buildSchema`, `handleCall`; `template.go` `arg` |
| L2 | Positional args are not visible to jq in pipelines (`when`, `extract`, `body_transform`, final filter). Only templated step fields see them. | `pipeline.go` (accVars seeded from service vars only) |
| L3 | `confirm:` steps cannot run over MCP, because `engine.Flags.Yes` is never set on that path, so the step always aborts. | `mcpserver.go` `executeAndRender` |
| L4 | A steps-only command with no command-level `method` is classified as read. It gets no audit record, keeps `readOnlyHint`, stays listed under `--read-only`, and gets the View, even when its steps POST. | `command.go` `isWrite("", "")` |
| L5 | A step `body` cannot be `@file`: it is template-expanded literally. The schema description says "Inline JSON or @file". | `pipeline.go` `runStep` vs `engine.go` `resolveBody` |
| L6 | A local override replaces the whole manifest. No field-level merge. | `load.go` |
| L7 | Pipeline steps are HTTP-only, even on a `jsonrpc-ws` service. Steps also ignore `path_rules`, pagination, `noauth`, and the request `codec` (body is always `application/json`). | `pipeline.go` `runStep` |
| L8 | Local manifests and `lint` decode **leniently**, so an unknown key (for example `paginaton:`) is silently dropped. Only catalog ingest enforces the closed schema. README's "an unknown key fails `anyctl lint`" is false. | `load.go` `decodeService`; `schemacheck.go` callers |
| L10 | Accepted but unimplemented values: auth `login-flow`/`external-tool` (fail at call time), `codec.response: hujson` (decoded as plain JSON), `output.mode: table` (renders as json). | `auth.go`, `render.go`, schema enums |
| L11 | `self-update` verifies against the release's own `.sha256` (integrity) but does not check the cosign signature (authenticity). | `selfupdate.go` |
| L12 | The MCP audit record has no caller identity, and generic-verb calls log empty `params` (path and body are not recorded). | `mcpserver.go` `auditMutation`, `handleVerb` |

Open issues (`jedwards1230/anyctl`):
- #63 — HTTPS + token (PAT) auth for private git catalogs. SSH is the only private path
  today.
- #60 — manifest layering/overlays. Declined; see §5.
- #30 — `catalog add <openapi-url>`: scaffold a manifest from an OpenAPI spec into the
  override dir.
- #49 — `svc post/put --no-expand` to send a body without template expansion.
- #62 — downstream sequencing around dropping the embedded catalog floor.
- #76 — adopt the MCP 2026-07-28 spec (go-sdk v1.7.0, stateless transport).
- #78 — no response-size or token-budget control for MCP callers.
- #79 — generic verb tools take a free-form `path` with no valid-path discovery.
- #80 — name positional command args in MCP schemas instead of `arg0…argN` (subsumed by
  the planned `inputs:`, §5).
- #81 — no per-command MCP tool filtering; only whole-service `--service` or global
  `--read-only`.
- #82 — the validate-catalog action can't be used at a released tag under a SHA-pinning
  ruleset.

## 5. Scope decisions and non-goals

Decided by the maintainer on 2026-10-11. Guiding phrase: **"keep it simple, but flexible
and easy to use in different ways."**

**Non-goals**
- **Cross-service composition.** No recipe or workflow manifests and no cross-service
  steps. Multi-service chaining belongs to n8n, scripts, or the calling agent.
- **Loops, for-each and parallel steps** (non-goal for now).
- **Manifest layering or overlays.** This excludes field-level merge (#60 option A),
  opinion overlays (#60 option B), and commands-only overlays. Reason: the maintainer owns
  their catalog and edits it directly. The escape hatches are a full local override (L6)
  and `annotations:`.
- **Growing the template grammar into a language** (P4).
- **Built-in policy or gating** beyond manifest `confirm:` (P1).

**Supported today:** single-service multi-step commands such as an "activity" summary or
add-by-title, using `steps` + `extract` + `vars` (§3.6).

**Planned — named command inputs (the only accepted feature direction)**

| Aspect | Decision |
|---|---|
| Scope | Per input: `name`, `description`, `required`, `default` (literal only). |
| Key | Working name `inputs:`, referenced as `{input.X}`. It can't be `params:`, which already holds jsonrpc params (`types.go` `Command.Params`). |
| CLI | Each input becomes a named flag. |
| MCP | Each input becomes a real schema property, with a `required` list. Fixes L1 for commands that declare inputs. |
| jq | Inputs are visible to pipeline jq. Fixes L2. |
| Compatibility | Positional `{argN}` keeps working unchanged. The schema change is additive (P6). |
| Deferred | Types, enums, escaping rules. |

**Planned small fixes:** L3 (confirm over MCP), L4 (classify a pipeline as write when any
step method writes), L5 (`@file` step bodies).

## 6. Success criteria

- A new service is one YAML file. Zero Go changes, and `lint --strict` plus a `--dry-run`
  confirm it before the first live call.
- The CLI and MCP produce the same request and the same filtered result for the same
  input. The only differences are the ones listed in §4.
- A catalog that passes `catalog validate` installs cleanly, carries no endpoints or
  credentials, and stays inert until profiled.
- Every failure maps to a documented exit class (`agentsafety.Classify`). No secret value
  ever appears in argv, verbose output, dry-run output, an error, or the audit log.
- CI stays green on `gofmt`, `go vet`, `golangci-lint`, `go test -race` (≥75% coverage),
  and `TestSchemaAndValidateAgree`.
