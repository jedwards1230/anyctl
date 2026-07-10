# full example

A profile-only config dir that binds **all 15 embedded services** to placeholder
hosts. There is no `services/` dir here — every service comes from the catalog
baked into the binary, and `profile.yaml` alone completes them. This is the
reference for a real multi-service setup.

```sh
export ANYCTL_CONFIG_DIR="$PWD/examples/full"

anyctl list                       # all 15 embedded services, each bound by profile.yaml
anyctl lint --strict              # every service is complete (base_url + secrets bound)
anyctl --dry-run svc radarr list  # preview a resolved request, send nothing
```

The `example.com` hosts, `192.0.2.x` addresses, and `op://vault/...` refs are
placeholders — replace them with your own before running against live services.
For the minimal, genuinely-runnable starting point, see
[`examples/quickstart/`](../quickstart/).
