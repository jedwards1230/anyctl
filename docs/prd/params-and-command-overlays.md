# PRD: Typed command inputs + commands-only overlays

Status: proposed · Scope: `jedwards1230/anyctl` · Resolves #60 narrowly (commands-only overlay; Options A/B deferred)

## Summary

Two additive manifest features, plus four small fixes found while scoping them:

1. **Typed command inputs.** A command declares named, typed inputs. They become CLI flags
   (`--title`), real properties in the MCP input schema (type, required, default, enum,
   description), `{input.title}` in templates, and `$inputs` in every jq site. Positional
   `{argN}` keeps working.
2. **Commands-only overlay.** A local file can add a command to a catalog service, or replace one
   wholesale by name, without forking the manifest. It cannot touch bindings, auth, secrets,
   vars or endpoints.
3. **Fixes:** `confirm:` steps over MCP, write classification of pipelines, step `body: @file`,
   and positional args visible to jq.

Steps stay scoped to one service. anyctl remains an unopinionated executor (CLAUDE.md, "Core
principle"), and templating stays "deliberately NOT a programming language" (template.go:5-7).

> **Naming deviation (forced):** the requested key `params:` is already taken on a command. It
> holds jsonrpc params (`Command.Params`, types.go:212; schema `manifest.schema.json:282`). This
> PRD uses **`inputs:`**, with `{input.X}` in templates (singular, mirroring `secrets:` →
> `{secret.X}`) and `$inputs` in jq. See Open questions.

## Problem

Today an agent-friendly write needs positional strings, has no required/typed contract on MCP,
and has to be forked to be customized. Current state, verified against source:

| Capability | Status | Evidence |
|---|---|---|
| Single-service pipelines (`steps:` with extract/when/confirm/on_error/capture_header) | Supported | pipeline.go:71-139, 270-282 |
| Named endpoints per step | Supported | pipeline.go:289-303 |
| Service vars in templates and pipeline jq (`.var`, `.env.var`) | Supported | pipeline.go:54-65, 83-89 |
| Per-machine vars from `profile.yaml` | Supported | profile.go:92-98 |
| Multi-endpoint "status" assembled by one output filter | Supported | pipeline.go:141-156; `sunshine.yaml` status in jedwards1230/home-orchestration `anyctl-catalog/` |
| Named / typed command inputs | **Not supported** | Positional only: template.go:37-40, 94-98 |
| Positional args in jq (`extract`/`when`/`body_transform`/output) | **Not supported** | accVars is seeded from vars only (pipeline.go:54-65). Args reach the template env only (pipeline.go:84-89) |
| JSON-safe body built from caller input | Partial | Only via `body_transform` over extracted data. Templating is raw substitution (template.go:46-81); see the radarr.yaml comment, lines 48-56 |
| MCP input schema | Partial | `arg0..N` are all `string`, generic description, `required: []` (mcpserver.go:74-108) |
| Missing input on MCP | **Silent** | Absent `argN` becomes `""` (mcpserver.go:476-482) |
| `confirm:` step over MCP | **Never runnable** | `executeAndRender` does not set `Flags.Yes` (mcpserver.go:588-597); the gate is at pipeline.go:103-105 |
| Write classification of a pipeline | Partial | `Write` uses only the command's `method` (command.go:56, 84-85). A steps command with no `method` is a read |
| Step `body: @file` | **Not supported** | Step body is template-expanded literally (pipeline.go:208-214). Compare `resolveBody` (engine.go:753-760) |
| Override a catalog service | Supported (whole file only) | load.go:186-226 (origin `override`) |
| Add or replace one command without forking | **Not supported** | No overlay mechanism exists |

Consequences of the write-classification gap:

- A no-`method` pipeline that POSTs gets `readOnlyHint: true` (annotations.go:27-28).
- It survives `mcp --read-only` (mcpserver.go:333).
- It emits no mutation audit record (mcpserver.go:824).
- It gets the read-only result View (mcpserver.go:356, 638).

The radarr catalog sets `method: POST` on its `add` pipeline purely as a workaround (radarr.yaml:63-66).

## Goals

- G1. A command can declare named, typed, documented inputs. CLI and MCP both enforce the same
  contract.
- G2. Inputs are usable in templates and in all manifest jq sites, so a caller-supplied value can
  build a JSON body safely via `body_transform`.
- G3. A user can add or replace a command on any loaded service with a small local file that
  survives `catalog update`. The portability boundary is unchanged.
- G4. Close the four small gaps listed above.
- G5. Additive only. Every existing manifest, profile and MCP tool name behaves identically.

## Non-goals

- **Cross-service composition.** No recipe or workflow manifests and no steps across services.
  Multi-service chaining belongs to n8n, scripts, or the agent.
- **Loops, for-each, parallel steps.** Possible future work, not designed here.
- **General field-level merge** of manifests (patching `auth`, `output`, one step, and so on). An
  overlay replaces a command wholesale or adds one. Nothing finer.
- **Opinion overlay (#60 Option B)** and #60 Option A as originally framed. Deferred.
- **Template language features.** No filters, conditionals or functions in `{…}`. Logic stays in
  jq. The only template change is the `{input.X}` namespace plus context escaping for it.
- Array/object input types, input-level `pattern`/`min`/`max`, and interactive prompts.
- Overlays inside installed catalogs. Overlays are local-only.

## Proposal

### 1. Typed inputs

#### 1.1 Schema shape (command level, additive)

```yaml
commands:
  add-title:
    help: add a movie by title
    inputs:
      title:     { type: string, required: true, description: "exact movie title" }
      year:      { type: int, description: "release year; disambiguates remakes" }
      monitored: { type: bool, default: true }
      availability:
        type: string
        enum: [announced, inCinemas, released]
        default: released
```

| Field | Type | Rule |
|---|---|---|
| key (input name) | string | `^[a-z][a-z0-9_]*$`; must not be a reserved name (§1.5) |
| `type` | `string` \| `int` \| `number` \| `bool` | required |
| `required` | bool | default `false`; `required: true` together with `default` is a validate error |
| `default` | scalar | must coerce to `type` and be in `enum` if one is set |
| `enum` | list of scalars | allowed on `string` and `int` only |
| `description` | string | shown in `--help` and in the MCP schema |

Go model: `Command.Inputs map[string]Input` (types.go) is carried onto `command.Command`
(command.go:15-33). JSON Schema: add `inputs` to `definitions.command` (manifest.schema.json:267-305),
which stays `additionalProperties: false`. `inputs:` on a single-request command and on a steps
command behave identically.

#### 1.2 Template access: `{input.X}`

Add an `input.` prefix to `resolveToken` (template.go:83-104), alongside `secret.` and `env.`. An
undeclared `{input.X}` is a **validate** error (load/lint time), not a runtime error. Rendering:
string as-is, int/number in decimal, bool as `true`/`false`, and an unset optional input as `""`.

**Context escaping, for `{input.X}` only.** Argument tokens stay raw for back-compat. Today a
search term must be hand-encoded: radarr `lookup` help says "spaces as +" (radarr.yaml:36-38).
Inputs fix that:

| Field | Escaping |
|---|---|
| `path` (command or step) | `url.PathEscape` |
| `query` (command or step) | `url.QueryEscape` |
| `headers`, `body`, jsonrpc `params` | none (raw) |

A literal `body:` or `params:` that embeds a string input stays injectable, just as with `{argN}`.
The documented pattern is `body_transform` with `$inputs`. Implementation: an `Escape func(string)
string` on `template.Env` (template.go:24-29), set per call site.

#### 1.3 jq access: `$inputs` and `$args`

Bind two jq variables in every manifest jq site:

- `extract` (input = response body, pipeline.go:272)
- `when` and `body_transform` (input = accVars, pipeline.go:93, 199)
- the pipeline output filter (pipeline.go:143)
- the single-request output filter (render.go:62, 126)
- the CLI `--filter` and MCP `filter` override

`$inputs` is an object holding every declared input after coercion and defaults; an unset
optional input is `null`. `$args` is the positional args as an array of strings. That closes the
gap that pipelines cannot see positional args today.

These are jq **variables**, not accVars keys, because `extract` runs against the response body and
not accVars. Variables also cannot collide with extracted names or the existing `env` key
(pipeline.go:65).

Plumbing: `filter.Compile` wraps `gojq.Parse` (filter.go:21-27). Parse-only queries cannot see
external variables. Add `filter.CompileWithVars(expr, names...)`, implemented with `gojq.Compile`
and `gojq.WithVariables`, plus a `RunWith(data, values...)`. Validation (validate.go:281-293)
compiles with the same names so `$inputs` and `$args` lint clean.

#### 1.4 CLI mapping

- Each input becomes a local cobra flag on the command (cli.go:247-256). The flag name is the
  input name with `_` replaced by `-`: `--title`, `--quality-profile`.
- Types: string → `--x v`; int and number are parsed (`--year 1999`); bool → `--monitored` or
  `--monitored=false`; enum values are checked after parsing.
- Positional args still pass through, so `{argN}` commands are unchanged.
- `svc <s> <cmd> --help` lists each input with its type, default, enum and description.
  `serviceHelp` marks commands that have required inputs.

#### 1.5 MCP mapping

`buildSchema` (mcpserver.go:77-116) emits one property per input:

```json
{"title":{"type":"string","description":"exact movie title"},
 "year":{"type":"integer"},
 "monitored":{"type":"boolean","default":true},
 "availability":{"type":"string","enum":["announced","inCinemas","released"],"default":"released"}}
```

Types map as `int` → `integer`, `number` → `number`, `bool` → `boolean`. `required` lists the
required inputs; today it is always empty (mcpserver.go:107). `arg0..N`, `filter`, `raw` and
`dry_run` remain as they are.

**Reserved names.** An input may not be named any of the following:

- MCP properties: `filter`, `raw`, `dry_run`, `confirm`, `arg<N>`
- CLI flags, after `_` → `-`: `config-dir`, `filter`, `raw`, `query`, `limit`, `output`,
  `endpoint`, `dry-run`, `verbose`, `json-errors`, `yes`, `help` (cli.go:113-123)

A collision is a validate error (exit 2).

#### 1.6 Validation and errors

One shared binder, `command.BindInputs(cmd, raw map[string]any) (map[string]any, error)`, is
called by both faces **before** `engine.Execute`. That ordering matters because a pipeline dry-run
returns early (pipeline.go:49-52). `engine.Request` (engine.go:45-53) gains `Inputs map[string]any`.

| Condition | CLI | MCP |
|---|---|---|
| Missing required input | usage error, exit 2 | `IsError`, class `usage` |
| Type coercion fails (`--year abc`) | exit 2 | `IsError`, `usage` |
| Value not in `enum` | exit 2, lists the allowed values | `IsError`, `usage`, lists the allowed values |
| Unknown flag | exit 2 (cobra; cli.go:389-396) | Unknown property is an error **only on commands that declare `inputs:`**. Commands without inputs keep ignoring extra keys |
| Dry-run | Validates, then the preview lists the resolved inputs | same |

Coercion: CLI values are strings and are always parsed. MCP accepts native JSON types, and also
accepts a string that parses cleanly (`"1999"` → 1999, `"true"` → true), because agents often
stringify. A non-integral number for `int` is an error.

The mutation audit record (mcpserver.go:823-836) adds the bound inputs as JSON next to the
positional `Params`. Inputs are caller data and never secrets, so recording them is safe.

### 2. Commands-only overlay

#### 2.1 File format and location

```yaml
# ~/.config/anyctl/overlays/radarr.yaml
extends: radarr            # bare name, or <catalog>:<service> to target one definer
commands:
  add-title: { … }         # new id → added; existing id → replaced wholesale
```

**Location: a new `<config-dir>/overlays/` directory, not `services/*.extend.yaml`.** The local
loop globs every `*.yaml` in `services/` (load.go:199-203) and decodes non-strictly (load.go:432).
An older binary would therefore load `services/radarr.extend.yaml` as a bogus service named
`radarr.extend`. Older binaries never read `overlays/`, since `Load` only reads `config.yaml`,
`profile.yaml`, `catalogs/` and `services/`.

Allowed top-level keys: `extends` (required) and `commands` (required, non-empty). Decoding is
strict (`KnownFields`, as profile.go:67-72 does). Any other key fails with a `*ConfigError`
(exit 2) naming the key, including `base_url`, `auth`, `secrets`, `vars`, `endpoints`,
`transport`, `spec` and `env_prefix`. This keeps #60's portability constraint: an overlay changes
what a service *does*, never where it points or how it authenticates.

Overlay commands may *reference* things the base declares:

- `endpoint:` names
- `{secret.X}` slot names
- `{var}` names, including profile-bound vars (profile.go:92-98)

Schema: a new embedded `schema/overlay.schema.json` that `$ref`s the manifest's `command`
definition. `anyctl schema overlay` prints it, extending the `ValidArgs` list at schema.go:33.

#### 2.2 Load, merge, precedence

The overlay pass runs in `Load` after the local `services/` loop (load.go:199-227) and before
`warnOrphanProfileBindings` (load.go:231):

1. Resolve `extends` with the same rules as `Loaded.Lookup` (load.go:82-97). An ambiguous bare
   name is an error asking for the qualified form. An unknown target is a **warning**, and the
   overlay is skipped (this mirrors the orphan-binding warning at load.go:360-374).
2. Copy the target `*Service`. Do not mutate it in place: the bare and qualified selectors share
   one pointer (load.go:328). For a bare target, re-point both selectors. For a qualified target,
   re-point only that selector.
3. For each overlay command, set `svc.Commands[id] = cmd`. That is a wholesale replace or add, and
   it wins over spec-inferred commands too, since those are merged at decode time (load.go:442).
4. Post-merge, run `validateCommand(id, cmd, svc)` (validate.go:187-217) on each overlay command.
   A failure is a Load error naming the overlay file (see Open questions on degrading instead).
5. Set the origin suffix `+extend`. Examples: `catalog:homelab+extend`, `override+extend`,
   `local+extend`.

**Precedence:** installed catalog < local full override (`services/<name>.yaml`) < overlay.
An overlay patches whichever definition won. At most **one overlay per target**; two overlays
naming one target is a duplicate error, matching two local files (load.go:215-216).

#### 2.3 `list` / `lint` / `catalog validate`

- `list` prints the origin with its suffix. The `%-9s` origin column (builtins.go:80) becomes a
  computed width.
- `lint` (all services) validates every overlay. `lint <file>` detects `extends:` and runs the
  standalone checks: schema, allowed keys, and per-command structure that does not need the base.
  `lint --strict` is unchanged (completeness is a service property).
- `catalog validate` and `catalog add` reject an `extends:` file. Overlays are local-only.

### 3. Small fixes

| # | Fix | Change |
|---|---|---|
| F1 | `confirm:` over MCP | When any step or `on_error` step has `confirm:`, `buildSchema` adds a boolean `confirm` property: "clears this command's confirm: gate(s)". `handleCall` maps it to `Flags.Yes`. Absent or false, the call still fails closed with the step's message. The error text at pipeline.go:104 names both faces ("pass --yes/-y, or confirm: true over MCP"), and so does the dry-run line at pipeline.go:344. Opt-in stays with the manifest, so the binary still gates nothing on its own. |
| F2 | Pipeline write classification | For a steps command, `Write = isWrite(cmd.Method) OR any step / on_error step is a write` (HTTP verb rule, command.go:83-88; steps run over HTTP only, pipeline.go:248). If every write step shares one method, pass it to `agentsafety.Hints`, otherwise pass `""` (annotations.go:42-44). An explicit command `method:` can only add write-ness, never remove it. The radarr `method: POST` workaround becomes redundant but stays harmless. |
| F3 | Step `body: @file` | Today `@path` is sent as the literal string (pipeline.go:208-214). Make it a validate error ("@file is not supported in a step body; use body_transform"). A file path is also non-portable. Parity support is deferred. Lowest priority. |
| F4 | Args in jq | Delivered by `$args` (§1.3). |

## Examples

### A. Simplified `radarr add` by title

**Before** (jedwards1230/home-orchestration `anyctl-catalog/radarr.yaml:57-91`): the caller must
first run `lookup`, copy a `tmdbId`, and pass it as `arg0`. The pipeline needs `method: POST` only
for classification.

**After** (catalog or overlay):

```yaml
  add-title:
    help: >-
      add ONE movie by exact title (optional year) and start searching. Errors without
      changing anything on no match, an ambiguous match, or a movie already in the library.
    inputs:
      title:     { type: string, required: true, description: "exact title, any case" }
      year:      { type: int, description: "release year; required when the title is ambiguous" }
      monitored: { type: bool, default: true }
    steps:
      - id: lookup
        path: /api/v3/movie/lookup
        query: term={input.title}            # query-escaped: no hand "+" encoding
        extract:
          movie: >-
            map(select((.title | ascii_downcase) == ($inputs.title | ascii_downcase)
                       and ($inputs.year == null or .year == $inputs.year)))
            | if length == 0 then error("no exact match for \($inputs.title)")
              elif length > 1 then error("\(length) matches; pass year: \(map(.year))")
              elif (.[0].id // 0) > 0 then error("already in library: \(.[0].title) (\(.[0].year))")
              else .[0] end
      - id: add
        method: POST                          # F2: makes the command a write; no top-level method needed
        path: /api/v3/movie
        body_transform: >-
          .quality_profile_id as $qp | .root_folder as $rf
          | .movie | del(.id, .path, .folder)
          + {qualityProfileId: ($qp | tonumber), rootFolderPath: $rf,
             monitored: $inputs.monitored, minimumAvailability: "released",
             addOptions: {monitor: "movieOnly", searchForMovie: true}}
        extract: { added: "{id, title, year, tmdbId, monitored}" }
    output: { filter: ".added" }
```

- CLI: `anyctl svc radarr add-title --title "The Thing" --year 1982`
- MCP: `radarr_add-title {"title":"The Thing","year":1982}`, with a schema that says `title` is
  required.
- The quality profile and root folder stay deployment vars bound in `profile.yaml`, never tool
  inputs. The body is still built by jq, so caller strings cannot inject keys (the rationale at
  radarr.yaml:48-56 holds).

### B. "What is radarr doing" (works today, no new features)

```yaml
  activity:
    help: queue + running/queued tasks + health, in one view
    steps:
      - id: queue
        path: /api/v3/queue
        extract:
          queue: "{total: .totalRecords, items: (.records | map({title, status, timeleft}))}"
      - id: tasks
        path: /api/v3/command
        extract:
          tasks: 'map(select(.status == "started" or .status == "queued") | {name, status, started})'
      - id: health
        path: /api/v3/health
        extract: { health: "map({type, message})" }
    output: { filter: "{queue, tasks, health}" }
```

This uses only shipped features: multiple GET steps, `extract`, and one output filter over
accVars. Precedent: `status` in jedwards1230/home-orchestration `anyctl-catalog/sunshine.yaml:39-57`.
With no `method`, it is correctly a read (and stays one under F2). Field names are illustrative;
verify them against Radarr's API.

### C. The same `add-title` as a user overlay

```yaml
# ~/.config/anyctl/overlays/radarr.yaml
extends: radarr
commands:
  add-title: { …identical to A… }
```

`anyctl list` then shows `radarr  catalog:homelab+extend`, and `catalog update homelab` leaves the
overlay in place. Adding `base_url:` or `vars:` to this file fails with exit 2.

## Back-compat and migration

- **Positional args:** `{argN}`/`{arg.N}`, `arg0..N` MCP properties and their silent-`""`
  behavior are unchanged. Inputs and args can be used together.
- **Existing manifests:** behave the same. `inputs:` and `{input.X}` are new keys and a new
  prefix. Edge case: a var literally named `input.<x>` would now be shadowed. No catalog manifest
  uses one; call it out in release notes.
- **F2** can flip a no-`method` pipeline that contains a write step from read to write. That
  changes its hints, adds an audit record, and drops it from `mcp --read-only`. This is the
  intended correction. Note it in release notes.
- **MCP:** tool names are unchanged. Schemas only gain properties. Unknown-property strictness
  applies only to commands that opt in via `inputs:`.
- **Schema:** stays additive (CLAUDE.md, Conventions). `anyctl schema` gains `inputs` on
  `command`. `anyctl schema overlay` is new.
- **Older binaries:** ignore `overlays/`. Manifests that use `inputs:` must not be installed on a
  binary that predates it, because it silently drops unknown keys at load (load.go:432). Catalog
  authors should bump `version` in the catalog index.

## Acceptance criteria

Inputs
- [ ] `inputs:` parses, is schema-validated, and rejects bad names, bad types, reserved
      names, `required`+`default` together, a default outside its enum, and undeclared `{input.X}`.
- [ ] CLI: `--title` etc. are registered and appear in `--help`. Missing required, a bad type,
      an out-of-enum value and an unknown flag all exit 2 with a clear message.
- [ ] MCP: properties carry type/description/default/enum, and `required` lists the required
      inputs. A missing required input, a bad type or an unknown property (on an input
      command) gives an `IsError` with class `usage`. Stringified scalars are coerced.
- [ ] `{input.X}` is path-escaped in `path`, query-escaped in `query`, and raw elsewhere. Tested
      with a value containing a space, `&`, `/`, and `"`.
- [ ] `$inputs` and `$args` resolve in extract, when, body_transform, the pipeline output filter,
      the single-request output filter, and `--filter`. Lint accepts them.
- [ ] Dry-run validates inputs first and prints the resolved inputs.
- [ ] The audit record includes the bound inputs.

Overlay
- [ ] `overlays/<f>.yaml` adds a new command and replaces an existing one wholesale. CLI and MCP
      both see the result.
- [ ] Any key other than `extends`/`commands` gives exit 2 naming the key.
- [ ] Post-merge validation catches an unknown `endpoint` and undeclared `{input.X}`. Two
      overlays on one target is an error. An unknown target warns and skips. An ambiguous bare
      target errors and lists the qualified forms.
- [ ] An overlay on a qualified target leaves the other catalog's definer untouched (no shared-pointer
      mutation).
- [ ] `list` shows the `+extend` origins. `lint <overlay-file>` and `anyctl schema overlay` work.
      `catalog validate` rejects an `extends:` file.

Fixes
- [ ] F1: a confirm-step command exposes `confirm`. Without it the call fails closed; with
      `confirm: true` it runs. Commands with no confirm step do not expose `confirm`.
- [ ] F2: a no-`method` pipeline with a POST step has `Write=true`, no readOnly hint, an audit
      record, and is dropped by `--read-only`. A GET-only pipeline stays read.
- [ ] F3: `@file` in a step body fails lint.
- [ ] Coverage stays ≥75% (CONTRIBUTING.md). `examples/` gains one inputs command and one overlay,
      and the examples lint test covers them.

## Rollout (independently shippable PRs)

1. **PR 1: fixes F1, F2, F3** (`semver:minor`, since F2 changes classification). Small, no new
   schema keys except F3's validate rule. Ships value immediately; radarr's workaround becomes
   optional.
2. **PR 2: typed inputs + `$args`** (`semver:minor`). Covers types, schema, binder, CLI flags, MCP
   schema, template prefix and escaping, and the jq variables in `filter`. README: add an
   "Inputs" subsection under *Manifests & profiles* and mention inputs in *MCP server*. CLAUDE.md:
   add `{input.X}` to the template line.
3. **PR 3: overlays** (`semver:minor`). Covers the `overlays/` loader, the overlay schema plus
   `schema overlay`, list/lint/catalog-validate handling, and an example. README: document
   `overlays/` in the config-dir tree (README:116-122), the precedence line (README:206-209) and
   the `list` origins. CLAUDE.md: update the Conventions origin list.
4. **Follow-up in jedwards1230/home-orchestration:** migrate `anyctl-catalog/radarr.yaml` to add
   `add-title` (or move it to inputs) once a release with PR 2 is deployed to the MCP server.

## Open questions

1. **Key name.** `params:` collides with jsonrpc `Command.Params` (types.go:212). This PRD uses
   `inputs:` / `{input.X}` / `$inputs`. Accept it, or prefer `flags:` or `arguments:`?
2. **Defaults that reference vars** (`default: "{quality_profile_id}"`)? Recommendation: no.
   jq already covers it (`$inputs.quality // .quality_profile_id` in `body_transform`), and keeping
   defaults literal keeps the MCP `default` honest.
3. **Can an overlay remove an inherited command, or `mcp_ignore` it,** without restating it?
   Today the workaround is to replace it wholesale with a copy that sets `mcp_ignore: true`. Is a
   `hide: [id…]` list worth adding?
4. **Overlay post-merge failure:** fail the whole `Load` (consistent with a malformed
   `services/` file, load.go:204-207), or degrade only that service with a warning (like spec
   inference, load.go:442-448)? This matters after a `catalog update` drops an endpoint that an
   overlay command uses.
5. **Literal `body:` with string inputs:** keep it raw (documented injection risk, same as
   `{argN}`), JSON-escape `{input.X}` in `body` (escaping without quotes), or reject string inputs
   in a literal body at lint?
6. **Overlay location:** confirm `overlays/<name>.yaml` over `services/<name>.extend.yaml`. The
   latter is mis-loaded by older binaries as a service named `<name>.extend`.
7. **MCP strictness:** should the unknown-property error for input commands also apply to
   commands without inputs in a future major release?
