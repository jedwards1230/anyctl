// Package schema serves the JSON Schema (draft-07) for a portable anyctl service
// manifest, compiled into the binary via go:embed. The schema is hand-authored
// (not struct-tag generated) because the manifest's rules — conditional required
// fields per auth strategy, and forbidding an in-manifest base_url / secret ref —
// cannot be faithfully expressed by struct-tag generation, and draft-07 is the
// dialect editors (yaml-language-server) support best. A conformance test guards
// the schema against drift from the Go model and the Validate rules.
//
// This package is intentionally dependency-free so it can be served by the CLI
// without pulling in extra modules. go:embed patterns cannot escape the package
// directory, so manifest.schema.json lives beside this file.
package schema

import _ "embed"

// Manifest is the raw JSON Schema (draft-07) for a portable service manifest.
//
//go:embed manifest.schema.json
var Manifest []byte

// Catalog is the raw JSON Schema (draft-07) for a catalog index file
// (anyctl-catalog.yaml) — the required identity/member record at the root of a
// dir or git catalog source. Like Manifest it is hand-authored so its rules
// (required name/description, member-filename pattern) stay editor-consumable.
//
//go:embed catalog.schema.json
var Catalog []byte
