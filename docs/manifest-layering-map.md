# anyctl manifest layering — design map (not a plan)

> Status: **map only, nothing to build yet** (decided 2026-07-10). Captures where the
> seams are so we can pick one to cut later. Audience we're designing for: **public
> catalog consumed by strangers** *and* **multi-tenant / per-deploy** reuse — both,
> so portability is a real constraint, not hypothetical.

## The question that started this

"Should we separate endpoint helpers from how auth is done? And what if a consumer
has a conflict with a key name we use — doesn't that invalidate the manifest for them?"

## What's already right

- **Portable manifest / per-machine `profile.yaml` / neutral executor** split is the
  correct spine. Keep it.
- **Auth credentials are already separated** — the manifest carries the auth
  *strategy* (`bearer`), the secret *value* binds in `profile.yaml`. So "how auth is
  done" is already two-part.
- **Service-name collisions are already solved** — two catalogs defining `n8n` both
  install, addressable as `<catalog>:n8n`; the bare name errors as ambiguous rather
  than silently picking. MCP tools rename to `<catalog>-n8n_…`.

## The reframe: the unseparated seam is facts vs. opinions, not auth vs. endpoints

A manifest today mixes three kinds of content:

| Kind | Examples | Portable? | Disputed? |
|---|---|---|---|
| **API facts** | paths, methods, auth *strategy*, pagination, `path_rules`, response shape | yes | rarely |
| **Opinions** | `output.filter` projections, `ui:` hints, which fields to show, command curation | yes | **often** |
| **Binding** | `base_url`, secret refs, endpoint/tls overrides → `profile.yaml` | no (per-machine) | n/a |

Auth *strategy* is a **fact** (Authentik *is* bearer) — co-locating it with endpoints
is fine, so a literal "auth vs. endpoints" split cuts along the wrong grain. The layer
that *isn't* separated is **opinions**, and it's exactly the layer consumers diverge on.

## The two collision surfaces behind the "key conflict" worry

1. **Schema-key namespace is closed.** Every object is `additionalProperties: false`,
   so a consumer can't add `x-owner:` / any annotation without `lint` rejecting it. The
   vocabulary is total and ours. This is the literal "invalidates it for them" hit.
   - Cheap fix: allow an `x-*` extension prefix (OpenAPI convention) or one reserved
     free-form `annotations:` map.
2. **No field-level merge on override** (the real portability killer). `catalog edit`
   seeds a **full copy**; a local override replaces wholesale, validated standalone.
   So disagreeing with *one* projection forces a **full fork**, then re-inheriting every
   upstream fact-fix by hand forever. The multi-tenant auth-method choice
   (API-key *or* OAuth per deployment) hits the same wall.

## Both worries collapse into one missing mechanism

A way to **diverge on a slice without forking the whole**. Options, smallest → largest:

- **`x-*` / `annotations` escape hatch** — opens the closed schema. Tiny. Addresses the
  literal key-conflict only.
- **Field-level merge on override** — keep one file; a local override patches specific
  fields instead of replacing. Solves the fork problem for facts *and* opinions.
- **Opinion overlay (highest leverage)** — split `output.filter` + `ui:` (+ optional
  per-deploy auth-method choice) into a mergeable layer keyed to the facts layer.
  Consumers keep our facts, swap our taste, add their own annotations, never fork;
  upstream fact-fixes still flow. Most work.

Because we're designing for both public + multi-tenant, the overlay is genuinely
warranted eventually — but deferred by choice for now.

## When we pick this up

Decide between "field-level merge" (smaller, one-file) vs. "opinion overlay" (cleaner,
two-file) — the escape hatch is worth doing regardless and independently. Revisit the
scrim canvas or this table; no code until then.
