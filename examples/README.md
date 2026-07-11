# examples

Three self-contained example config dirs. Point `ANYCTL_CONFIG_DIR` at whichever
one you want to try.

| Dir | What it shows |
|-----|---------------|
| [`quickstart/`](quickstart/) | The smallest working config — one hand-written manifest bound to a public, no-auth API. Runs end-to-end with no LAN or secrets. **Start here.** |
| [`full/`](full/) | An installed reference catalog (`catalogs/reference/`) plus a profile binding its two services to placeholder hosts — the reference for a real multi-service setup. |
| [`catalog/`](catalog/) | A minimal third-party **catalog** (a shareable set of portable manifests), used as the reference for `anyctl catalog add` / `catalog validate`. |
