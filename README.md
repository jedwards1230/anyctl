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

**anyctl ships with no built-in services.** You give it something to run in one of
two ways: **install a catalog** — a bundle of portable manifests from a directory
or git repo (`anyctl catalog add <source>`) — or **write a local manifest** under
`services/`. Either way you then bind the services you use to your machine with a
`profile.yaml`. With nothing configured, `anyctl list` prints a short hint telling
you how to configure one.

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

Every manifest field is otherwise closed — an unknown key fails `anyctl lint`.
The one exception is a reserved `annotations:` map (top-level and per-command):
free-form consumer metadata that lets you annotate a manifest without forking it.
anyctl never validates or reads it, and it never affects execution.

```yaml
name: radarr
annotations:
  owner: platform-team
  tags: [media, critical]
```

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
[`quickstart/`](examples/quickstart/) (one no-auth local service, runs as-is),
[`full/`](examples/full/) (an installed reference catalog + a profile binding its
two services to placeholder hosts — `ANYCTL_CONFIG_DIR=examples/full anyctl lint
--strict`), and [`catalog/`](examples/catalog/) (a reference third-party
catalog).

## Commands

```sh
anyctl list                           # every service + its origin (local / override / catalog:<name>)
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

anyctl ships with **no built-in services**. You **install named catalogs** —
bundles of portable manifests published as a git repo (any forge: GitHub,
Forgejo, Codeberg, GitLab, …) or a local directory — into `catalogs/<name>/`.
**Git is the default distribution path.**

Every catalog source carries a required **`anyctl-catalog.yaml`** index at its
root — the identity record that makes it installable:

```yaml
# anyctl-catalog.yaml
name: my-catalog                 # required; the default install name (^[a-z0-9][a-z0-9_-]*$)
description: what this provides  # required; a one-line summary
version: "1.0.0"                 # optional, informational
homepage: https://git.example/you/my-catalog   # optional
manifests:                       # optional — omit to auto-glob every top-level *.yaml
  - uptime.yaml                  # …or list members to curate + order them
  - inventory.yaml
```

```sh
anyctl catalog add https://git.example/you/my-catalog.git         # install from any git host
anyctl catalog add https://git.example/team/cat.git --ref v1.2    # …pinned to a ref
anyctl catalog add https://git.example/org/infra.git --path anyctl-catalog  # …from a repo subdir
anyctl catalog add ./my-manifests                                 # …or a local dir
anyctl catalog add ./openapi.json --openapi                       # …or from an OpenAPI 3.x doc (no index needed)
anyctl catalog list                                               # name, version, services, pinned commit, source
anyctl catalog info <name>                                        # one catalog's full detail + its services
anyctl catalog update [name...]                                   # re-fetch (variadic; all if none named)
anyctl catalog remove <name>
```

The install name is `--name`, else the index's `name`. Get the index schema for
editor validation with `anyctl schema catalog`.

**Resolution precedence (highest wins):** local `services/<name>.yaml` >
installed catalog. There is no built-in floor — with neither present, a config
has no services. Two installed catalogs may share a service name; the bare name
then becomes ambiguous and you address each as `<catalog>:<service>`.

A catalog carries no endpoints or credentials, so it's **inert until your
profile binds it** — that's why catalogs need no signing. `catalog add`
validates the index and every member manifest against the schema + portability
rule before writing anything; a git source is pinned to its commit SHA for
reproducibility. When a repo keeps its catalog in a subdirectory, pass `--path
<subdir>` to install from there (the index must live in that subdir) — the
subdir is recorded so `catalog update` re-fetches from the same place. `--path`
is git-only and must stay within the repo (no absolute path, no `..`).

**Publishing your own:** push a repo with an `anyctl-catalog.yaml` index plus
your manifests. Check it against anyctl's contract before anyone installs it:

```sh
anyctl catalog validate ./my-catalog   # read-only: index + schema + portability check, exit 2 on failure
```

Wire that into CI with the bundled composite action
(`jedwards1230/anyctl/.github/actions/validate-catalog@v0.21.0` — pin to the
current release tag; see the [releases](https://github.com/jedwards1230/anyctl/releases)).
[`examples/catalog/`](examples/catalog/) is a minimal reference catalog.

### Private catalogs over SSH

A private catalog installs over SSH — `git@host:you/cat.git` (scp-style) or
`ssh://git@host/you/cat.git`. anyctl runs system `git`, so SSH auth defers
entirely to your environment: `~/.ssh/config` (host, user, `IdentityFile`) and a
running `ssh-agent`. Set up a key with your forge's normal flow — GitHub deploy
keys or a user SSH key, and the Forgejo/Codeberg/GitLab equivalents (a repo
deploy key or an account SSH key).

anyctl sets `GIT_TERMINAL_PROMPT=0`, so git **never hangs on a prompt** and fails
closed instead. A consequence: a **passphrase-protected key needs a loaded
`ssh-agent`** (`ssh-add ~/.ssh/id_ed25519`) — with no agent, git can't read the
passphrase non-interactively and the add fails cleanly rather than blocking.

Triage `Permission denied (publickey)`:

- `ssh -T git@host` — confirm the key authenticates to the forge at all.
- `ssh-add -l` — confirm the right key is loaded in the agent.
- Check `~/.ssh/config` maps the host to the intended `IdentityFile`/user.
- Confirm the (deploy) key is authorized for *that* repo on the forge.

HTTPS with a token (PAT) is **not yet supported** — tracked in
[TRACKING-ISSUE-URL](TRACKING-ISSUE-URL). Use SSH for private catalogs today.

## Secrets

Secrets are always **references** resolved at call time — a manifest stores
`op://vault/item/field`, never a value, and resolved secrets never appear in
argv or logs.

**Zero-dependency default — env override.** The simplest path needs no external
tool at all: with `env_override` on, a `<PREFIX>_<SECRET>` env var is used
verbatim and skips resolution entirely (`<PREFIX>` is the service's `env_prefix`,
`<SECRET>` the slot name — e.g. `RADARR_API_KEY`). Ideal for CI, containers, and
Kubernetes/Docker secret env.

**Providers** cover everything else. `config.yaml` declares them, dispatched by a
ref's URI scheme:

```yaml
secrets:
  env_override: true            # <PREFIX>_<SECRET> env skips resolution
  providers:
    # 1Password (op://…) — the default; item idioms + optional service-account token.
    onepassword:
      scheme: op
      command: ["op", "read", "{ref}"]
      auth:
        service_account_token:            # optional; omit to use the desktop op session
          file: ~/.config/anyctl/sa-token # exactly one of file | value | env

    # Generic exec — any {ref}-templated command's stdout (pass, vault, sops, …).
    pass:
      type: exec
      command: ["pass", "show", "{ref}"]  # a pass://vault/item ref → its stdout

    # File — read a value from an owner-only file (0600/0400).
    file:
      type: file                          # a file:///run/secrets/token ref
```

An op `service_account_token.file` and every `file://` path must be owner-only
(`0600`/`0400`) or they're refused. If a provider's binary isn't on `PATH`, the
error names the provider and points you at the `<PREFIX>_<SECRET>` env override.
Most new backends need no Go — a config-only `exec` or `file` provider covers
them; a bespoke one is a few edits in
[`internal/secret/provider.go`](internal/secret/provider.go), no engine changes.

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
