package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// CatalogIndexFile is the well-known index file every dir/git catalog source
// MUST carry at its root (the --path subdir for a subdir install). It is the
// intentional-publish marker and identity record: a bare directory of *.yaml is
// no longer installable without it. The one fixed filename IS the discovery
// contract — there is no flag to rename it; --path locates it within a repo. It
// is NOT copied into the installed catalog dir (its fields fold into
// CatalogMetaFile), so the loader never sees it.
const CatalogIndexFile = brand.Name + "-catalog.yaml"

// ErrCatalogIndexMissing is returned by ReadCatalogIndex when a source has no
// CatalogIndexFile at its root. Callers detect it with errors.Is to print an
// actionable "add anyctl-catalog.yaml" hint (with a starter snippet) instead of
// a bare read error.
var ErrCatalogIndexMissing = errors.New("catalog index " + CatalogIndexFile + " not found")

// CatalogIndex is the parsed anyctl-catalog.yaml: a catalog's identity plus an
// optional curated member list. name/description are required; version/homepage
// are informational and shown by `catalog list`/`info`. It is the forward
// extension point (min-version, signing) that keeps the manifest schema itself
// untouched.
type CatalogIndex struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version,omitempty"`
	Homepage    string `yaml:"homepage,omitempty"`
	// Manifests, when non-empty, is the exact ordered set of member manifests to
	// install (bare .yaml/.yml filenames, no path separators). Empty/absent means
	// "auto-glob every top-level *.yaml/*.yml except the index itself".
	Manifests []string `yaml:"manifests,omitempty"`
}

var (
	compiledCatalogSchemaOnce sync.Once
	compiledCatalogSchema     *jsonschema.Schema
	compiledCatalogSchemaErr  error
)

// catalogSchemaURL is the $id the embedded catalog-index schema registers under,
// mirroring manifestSchemaURL in schemacheck.go.
const catalogSchemaURL = "https://raw.githubusercontent.com/" + brand.Repo + "/main/schema/catalog.schema.json"

// catalogSchema compiles the embedded catalog-index schema exactly once (pure,
// never changes at runtime) and caches the result, mirroring manifestSchema().
func catalogSchema() (*jsonschema.Schema, error) {
	compiledCatalogSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema.Catalog))
		if err != nil {
			compiledCatalogSchemaErr = fmt.Errorf("unmarshal embedded catalog schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource(catalogSchemaURL, doc); err != nil {
			compiledCatalogSchemaErr = fmt.Errorf("add catalog schema resource: %w", err)
			return
		}
		sch, err := c.Compile(catalogSchemaURL)
		if err != nil {
			compiledCatalogSchemaErr = fmt.Errorf("compile catalog schema: %w", err)
			return
		}
		compiledCatalogSchema = sch
	})
	return compiledCatalogSchema, compiledCatalogSchemaErr
}

// ParseCatalogIndex validates and decodes an anyctl-catalog.yaml body. It is the
// fail-closed identity gate `catalog add`/`validate` run: JSON Schema (the same
// one `anyctl schema catalog` prints), strict decode (a typo'd key is an error),
// then the semantic rules the schema pins but Go owns as the source of truth —
// name is a valid single path segment (ValidateName), description is present,
// and every manifests entry is a bare .yaml/.yml filename (no separators, no
// traversal). Every failure is a *ConfigError (exit 2).
func ParseCatalogIndex(data []byte) (*CatalogIndex, error) {
	sch, err := catalogSchema()
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("parse %s: %w", CatalogIndexFile, err)}
	}
	if err := sch.Validate(generic); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("%s schema: %w", CatalogIndexFile, err)}
	}

	// Strict decode: mirror config.yaml's KnownFields so an unknown key is a hard
	// error rather than a silently-dropped field. An empty file (io.EOF) still
	// fails the required-field checks below, so it is caught either way.
	var idx CatalogIndex
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&idx); err != nil && !errors.Is(err, io.EOF) {
		return nil, &ConfigError{Err: fmt.Errorf("parse %s: %w", CatalogIndexFile, err)}
	}

	if err := ValidateName(idx.Name); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("%s: %w", CatalogIndexFile, err)}
	}
	if idx.Description == "" {
		return nil, &ConfigError{Err: fmt.Errorf("%s: description is required (a one-line summary)", CatalogIndexFile)}
	}
	for _, m := range idx.Manifests {
		if err := validateMemberFilename(m); err != nil {
			return nil, &ConfigError{Err: fmt.Errorf("%s: %w", CatalogIndexFile, err)}
		}
	}
	return &idx, nil
}

// validateMemberFilename enforces that a manifests: entry is a bare .yaml/.yml
// path segment — no separators, no "."/".." — matching InstallCatalog's own
// files-map key guard so a curated member can never point outside the source.
func validateMemberFilename(name string) error {
	if name == "" {
		return fmt.Errorf("empty manifests entry")
	}
	if base := filepath.Base(name); base != name || base == "." || base == ".." {
		return fmt.Errorf("invalid manifests entry %q: must be a bare file name (no path separators)", name)
	}
	if !isYAML(name) {
		return fmt.Errorf("invalid manifests entry %q: must end in .yaml or .yml", name)
	}
	return nil
}

// ReadCatalogIndex reads and parses the CatalogIndexFile at dir's root. A missing
// index is ErrCatalogIndexMissing (detect with errors.Is) so callers can print a
// starter snippet; any other failure is the parse/validation error from
// ParseCatalogIndex.
func ReadCatalogIndex(dir string) (*CatalogIndex, error) {
	path := filepath.Join(dir, CatalogIndexFile)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrCatalogIndexMissing
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseCatalogIndex(b)
}
