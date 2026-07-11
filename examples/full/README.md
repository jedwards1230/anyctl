# full example

A multi-service config dir: an **installed catalog** under `catalogs/reference/`
plus a `profile.yaml` that binds both of its services to placeholder hosts.
anyctl ships with no built-in services, so this is the reference for a real setup
— install a catalog (or add local `services/`), then bind it with a profile.

The `catalogs/reference/` dir holds the two generic manifests from
[`examples/catalog/`](../catalog/) — exactly what
`anyctl catalog add examples/catalog --name reference` produces at rest (an
installed catalog is just its YAML files under `catalogs/<name>/`).

```sh
export ANYCTL_CONFIG_DIR="$PWD/examples/full"

anyctl list                       # 2 services, origin catalog:reference
anyctl lint --strict              # every service is complete (base_url + secrets bound)
anyctl --dry-run svc uptime status  # preview a resolved request, send nothing
```

The `example.com` hosts are placeholders — replace them with your own before
running against live services. For the minimal, genuinely-runnable starting
point, see [`examples/quickstart/`](../quickstart/).
