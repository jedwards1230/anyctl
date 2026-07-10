# anyctl

A single, manifest-driven CLI for HTTP/RPC service APIs. **A service is one YAML
file** — the binary knows nothing service-specific, so adding or removing a
service is a manifest edit, never a recompile.

It replaces a pile of bespoke per-service `curl`/`jq`/auth/pagination wrappers
with one static Go binary you can run at a shell, call from an agent over the
CLI, or expose to an agent over MCP — all from the same config.

## Install

```sh
go install github.com/jedwards1230/anyctl@latest   # or grab a static binary from Releases
anyctl self-update            # update in place to the latest release (sha256-verified)
anyctl self-update --check    # report current vs latest, download nothing
```

## Quick start

The fastest way to see it work needs no LAN and no secrets — the
[`examples/quickstart/`](examples/quickstart/) config binds one hand-written
manifest to the public, no-auth [httpbin.org](https://httpbin.org):

```sh
export ANYCTL_CONFIG_DIR="$PWD/examples/quickstart"
anyctl svc httpbin get                # a real request against httpbin.org
anyctl lint --strict httpbin          # confirm httpbin's base_url is bound
```

Then set up your own config dir:

```sh
anyctl init                           # provision ~/.config/anyctl (config.yaml + profile.yaml)
# bind a service in profile.yaml (see below), then:
anyctl lint --strict radarr           # confirm that one service's base_url + secrets are bound
anyctl svc radarr status              # smoke-test the live endpoint
anyctl svc radarr list --dry-run      # preview the resolved request, send nothing
```

**You don't need to write any manifests to start.** 15 services ship embedded in
the binary — radarr, sonarr, prowlarr, bazarr, tdarr, n8n, authentik, harbor,
abs, forgejo, sunshine, truenas, ts, contextforge, cloudflare. `anyctl list`
shows them all. You just bind the ones you use to your machine with a
`profile.yaml`.

## Manifests & profiles

A **manifest** is *portable*: it declares what a service is (commands, auth
strategy, secret slots) and is identical for everyone. It carries **no**
endpoint or credentials.

```yaml
# a portable manifest — identical for every user
name: radarr
env_prefix: RADARR
auth: { strategy: header-key, header: X-Api-Key, value: "{secret.api_key}" }
secrets:
  api_key: { env: RADARR_API_KEY }   # slot declared; bound in profile.yaml
commands:
  list: { method: GET, path: /api/v3/movie }
```

Your **`profile.yaml`** binds each service to *this* machine — its `base_url`
and secret refs:

```yaml
version: 1
services:
  radarr:
    base_url: https://movies.example.com
    secrets:
      api_key: { ref: "op://vault/Radarr/api_key" }
```

That split is the whole model. Binding lives **only** in `profile.yaml` (or a
`<PREFIX>_URL` / `<PREFIX>_<SECRET>` env override, which wins over the profile).
A manifest that carries a `base_url` or secret `ref` is rejected by `anyctl lint`
— it points you at the profile slot to use instead.

Config lives under `$XDG_CONFIG_HOME/anyctl` (or `~/.config/anyctl`); override
with `ANYCTL_CONFIG_DIR` or `--config-dir`:

```
~/.config/anyctl/
├── config.yaml     # global defaults + secret providers
├── profile.yaml    # per-machine binding (base_url + secret refs)
├── services/       # optional: local overrides or new services
└── catalogs/       # optional: installed named catalogs
```

The [`examples/`](examples/) dir has three ready-to-try configs:
[`quickstart/`](examples/quickstart/) (one no-auth service, runs as-is),
[`full/`](examples/full/) (a profile-only config binding all 15 embedded
services to placeholder hosts — `ANYCTL_CONFIG_DIR=examples/full anyctl lint
--strict`), and [`catalog/`](examples/catalog/) (a reference third-party
catalog).

## Commands

```sh
anyctl list                           # every service + its origin (embedded / local / override / catalog:<name>)
anyctl svc radarr list                # run a named command
anyctl svc radarr list --filter 'length'   # gojq filter the output
anyctl svc radarr list --dry-run      # print the resolved request, send nothing
anyctl svc tdarr get /api/v2/status   # generic verb passthrough (get/post/put/patch/delete)
anyctl s radarr list                  # `s` is shorthand for `svc`

anyctl doctor                         # probe each service's reachability
anyctl lint                           # validate every manifest's schema
anyctl lint --strict                  # also require completeness (bound base_url + secrets)
anyctl init myservice --auth bearer -o services/myservice.yaml   # scaffold a starter manifest
anyctl schema > manifest.schema.json  # JSON Schema for editor completion/validation
```

Service commands live under `svc` (aliased `s`) so a user-defined service can
never collide with a built-in like `list` or `doctor`.

## Catalogs

The 15 embedded manifests are the **default catalog** — the floor every install
gets for free. A manifest is plain YAML, so editing one is **rebuild-free**:

```sh
anyctl catalog show radarr            # dump an embedded manifest
anyctl catalog edit radarr            # copy it into services/ for live editing (no recompile)
anyctl catalog vendor radarr --catalog-dir ./catalog   # promote an edit back into a repo checkout to ship
```

`catalog edit` seeds a **full** copy (not a patch) because a local override
wholesale replaces the embedded entry.

You can also **install named catalogs** — bundles of portable manifests from a
directory or git repo — into `catalogs/<name>/`:

```sh
anyctl catalog add ./my-manifests                    # install a local dir (name = basename)
anyctl catalog add https://git.example/team/cat.git --ref v1.2   # …or a git repo, pinned to a ref
anyctl catalog add ./openapi.json --openapi          # …or materialize one from an OpenAPI 3.x doc
anyctl catalog installed                             # list installed catalogs
anyctl catalog update [name]                         # re-fetch from the recorded source
anyctl catalog remove <name>
```

**Resolution precedence (highest wins):** local `services/<name>.yaml` >
installed catalog > embedded. Two installed catalogs may share a service name;
the bare name then becomes ambiguous and you address each as
`<catalog>:<service>`.

A catalog carries no endpoints or credentials, so it's **inert until your
profile binds it** — that's why catalogs need no signing. `catalog add`
validates every manifest against the schema + portability rule before writing
anything; a git source is pinned to its commit SHA for reproducibility.

**Publishing your own:** any git repo or directory of portable manifests is a
valid source. Check it against anyctl's contract before anyone installs it:

```sh
anyctl catalog validate ./my-manifests   # read-only schema + portability check, exit 2 on failure
```

Wire that into CI with the bundled composite action
(`jedwards1230/anyctl/.github/actions/validate-catalog@v1`).
[`examples/catalog/`](examples/catalog/) is a minimal reference catalog.

## Secrets

`config.yaml` declares secret providers, dispatched by a ref's URI scheme
(`op://` → the 1Password provider):

```yaml
secrets:
  env_override: true            # allow <PREFIX>_<SECRET> env to skip resolution
  providers:
    onepassword:
      scheme: op
      command: ["op", "read", "{ref}"]
      auth:
        service_account_token:            # optional; omit to use the desktop op session
          file: ~/.config/anyctl/sa-token # exactly one of file | value | env
```

Secrets are always **references** resolved at call time — a manifest stores
`op://vault/item/field`, never a value, and secrets never appear in argv or
logs. A `service_account_token.file` must be owner-only (`0600`/`0400`) or it's
refused. Adding a backend (`aws://`, `vault://`, …) is a few edits in
[`internal/secret/provider.go`](internal/secret/provider.go) — no engine changes.

## How it works

- **One executor, two faces.** The CLI and the MCP server both drive the same
  engine, so behavior is identical.
- **Transports:** `http` (curl-equivalent) and `jsonrpc-ws` (WebSocket JSON-RPC,
  used by TrueNAS).
- **Auth:** `none`, `header-key`, `bearer`, `basic`, `oauth2-client-credentials`
  (with token cache), and `ws-login`.
- **Two command sources:** hand-written `commands:`, or OpenAPI inference from a
  `spec:`. A command can also declare `steps:` for composed pipelines
  (extract → feed the next request, with `when:`/`confirm:`/`on_error:`).
- **Unopinionated executor.** The binary gates nothing — it does exactly what the
  manifest says — **except** a step explicitly marked `confirm:`, which aborts
  unless `--yes` clears it. Guardrails belong in the consuming layer, not the tool.
- **Unix-native.** stdout is data, stderr is diagnostics, exit codes are real,
  secrets never touch argv, manifests are re-read per call.

## MCP server

`anyctl mcp` exposes every command as a tool over stdio (default) or
streamable-HTTP (`anyctl mcp --http :9000`, endpoint `/mcp`, `GET /healthz`
probe). It also exposes the generic verbs (`<svc>_get/_post/…`, `<svc>_call`)
so an agent gets the full write surface; `--read-only` drops the write verbs and
`--service` restricts to named services.

Read tools additionally link an [MCP Apps](https://github.com/modelcontextprotocol/ext-apps)
result View — a universal table/record/tree HTML view a supporting host renders
inline; other hosts fall back to the plain-text result. A command can shape its
rendering with an optional `ui:` hint block (`view`, `columns`, `primary`,
`badges`, `sort`, `drilldown`); absent one, the View auto-detects by result shape.

**Security:** a `--http` bind to loopback is unauthenticated (network
reachability is the boundary). A bind to any non-loopback address *refuses to
start* without a bearer token (`--auth-token-file` or `ANYCTL_MCP_AUTH_TOKEN`,
constant-time compared) unless `--allow-unauthenticated` opts out. The
[`anyctl-mcp` chart](deploy/helm/anyctl-mcp) adds an opt-in NetworkPolicy.

## Observability

Tracing is **off by default** and costs nothing unless the standard `OTEL_*` env
configures an OTLP endpoint. When set, each invocation emits one span
(`<service> <command>`) so parallel-agent calls are traceable in Tempo/Jaeger:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
anyctl svc radarr list
```

Export is fail-open and flush is time-bounded — a slow or down collector never
hangs a command. Prefer an HTTPS/TLS collector; plain `http://` sends spans in
cleartext.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build/test/lint commands, the
manifest-authoring loop, and the release process.

## License

MIT. Studies patterns from [`rest-sh/restish`](https://github.com/rest-sh/restish)
(MIT) — see [NOTICE](NOTICE).
