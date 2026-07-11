# quickstart example

The smallest working anyctl config: one hand-written portable manifest
(`services/httpbin.yaml`) bound to a public, no-auth API in `profile.yaml`.
Because [httpbin.org](https://httpbin.org) is public and needs no credentials,
this runs end-to-end with no LAN and no secret store.

```sh
export ANYCTL_CONFIG_DIR="$PWD/examples/quickstart"

anyctl list                          # httpbin (local) — the only configured service
anyctl lint                          # structural check — all pass
anyctl lint --strict httpbin         # confirm httpbin's base_url is bound
anyctl svc httpbin get               # real request against httpbin.org
anyctl svc httpbin uuid              # -> a random UUID
anyctl svc httpbin get --dry-run     # preview the resolved request, send nothing
```

Next steps:

- Add another service: write `services/<name>.yaml` (see `anyctl init <name>`
  for a starter) and bind it in `profile.yaml`.
- Install a shared catalog instead of writing your own — see
  [`examples/full/`](../full/), which installs a reference catalog and binds it.
