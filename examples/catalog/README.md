# Example catalog

A minimal, two-service reference catalog demonstrating the shape a third-party
`anyctl` catalog repo should ship: the required
[`anyctl-catalog.yaml`](anyctl-catalog.yaml) index, one no-auth service
([`uptime.yaml`](uptime.yaml)), and one header-key service
([`inventory.yaml`](inventory.yaml)). Both manifests are placeholders
(`example.com`) and the whole catalog passes
`anyctl catalog validate examples/catalog`.

## The catalog index

Every dir/git catalog source MUST carry `anyctl-catalog.yaml` at its root — the
identity record that makes it installable (a bare directory of `*.yaml` no longer
is). It names and describes the catalog and, optionally, curates its members:

```yaml
name: reference               # required; the default install name (^[a-z0-9][a-z0-9_-]*$)
description: two-service ...   # required; a one-line summary
version: "1.0.0"              # optional, informational (shown by `catalog list`/`info`)
homepage: https://...         # optional
manifests:                    # optional — omit to auto-glob every top-level *.yaml (except the index)
  - uptime.yaml               #   …or list members to curate + order them
  - inventory.yaml
```

Get the index's JSON Schema for editor validation with `anyctl schema catalog`.

This directory is deliberately **not** `examples/catalogs/` (plural) — that
path is reserved for an *installed* catalog under a config dir. This is just a
reference checked by CI (`internal/manifest/example_catalog_test.go` and the
`validate-catalog-action` job in `.github/workflows/ci.yml`); it is never
auto-loaded by `anyctl`.

## Writing a portable manifest

A catalog manifest declares *what* a service is — its commands, auth strategy,
and secret slots — and nothing machine-specific:

```yaml
name: inventory
description: example header-key service — an inventory/warehouse API
env_prefix: INVENTORY

auth:
  strategy: header-key
  header: X-Api-Key
  value: "{secret.api_key}"

secrets:
  api_key:
    env: INVENTORY_API_KEY   # declared here; bound in the CONSUMER's profile.yaml

commands:
  items:
    help: list inventory items
    method: GET
    path: /items
```

**A manifest must NOT carry a `base_url` (service or endpoint) or a secret
`ref`.** Those are machine-specific bindings that live only in the *consumer's*
`profile.yaml` — never in the catalog. `anyctl catalog validate` /
`catalog add` enforce this (structural `Validate`, the same gate either way)
and reject anything that carries one. An in-manifest secret `env:` (like
`INVENTORY_API_KEY` above) is fine — it just declares where an env-override
*could* supply the secret; it still resolves from the consumer's environment,
never a value baked into the manifest.

See [`anyctl schema`](../../README.md#manifest-json-schema-editor-support) for
the full JSON Schema. For fuller real-world examples (header-key, bearer, basic
auth; named commands; pagination; multi-endpoint), see the manifests in any
published catalog.

## Validating before you publish

```sh
anyctl catalog validate .   # run from this directory, or pass any catalog dir
```

Read-only: no network call, no config dir, no install. Exits 0 only if the
`anyctl-catalog.yaml` index is present and valid and every selected member
manifest is a valid, portable manifest with no two sharing a service name.

### CI: the validate-catalog action

Wire the bundled composite action into your catalog repo's own CI so a broken
manifest fails the PR instead of breaking a consumer's `catalog add`:

```yaml
# .github/workflows/validate.yml
on: [push, pull_request]
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: jedwards1230/anyctl/.github/actions/validate-catalog@v0.21.0  # pin to a current release tag
        with:
          path: .          # default "."; the dir holding your *.yaml manifests
          version: latest  # default "latest"; pin to a anyctl release if you need stability
```

## How a consumer installs and uses it

```sh
anyctl catalog add https://github.com/you/your-catalog.git --name yours
anyctl catalog add ./local-checkout --name yours          # …or a local dir
anyctl catalog list                                       # confirm it's there
anyctl catalog info yours                                  # its identity + services

anyctl svc inventory items                                # address by bare name
anyctl svc yours:inventory items                           # …or the qualified <catalog>:<service> form
```

The qualified `<catalog>:<service>` selector always works. The bare name only
works while it's unambiguous — if a consumer has another installed catalog that
also defines a service named `inventory`, the bare name errors and lists both
qualified forms instead of silently picking one. Resolution precedence (highest
wins): a consumer's local `services/<name>.yaml` \> any installed catalog. There
is no built-in floor.

An installed catalog is **inert** until the consumer's `profile.yaml` binds a
`base_url` and the declared secrets — installing it only makes the manifests
*available*, the same `anyctl` [unopinionated executor](../../README.md#how-it-works)
principle as everywhere else.
