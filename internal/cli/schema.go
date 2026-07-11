package cli

import (
	"fmt"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/jedwards1230/anyctl/schema"
	"github.com/spf13/cobra"
)

// cmdSchema prints an embedded JSON Schema (draft-07) to stdout: the portable
// service manifest by default, or the catalog index (anyctl-catalog.yaml) with
// the `catalog` arg. Pipe either to a file and point an editor's
// yaml-language-server at it for completion + validation while authoring.
func (r *runner) cmdSchema() *cobra.Command {
	return &cobra.Command{
		Use:   "schema [manifest|catalog]",
		Short: "Print the manifest or catalog-index JSON Schema (draft-07)",
		Long: "Print a JSON Schema (draft-07) to stdout. With no argument (or `manifest`)\n" +
			"it prints the PORTABLE service-manifest schema; with `catalog` it prints the\n" +
			"catalog-index schema for " + manifest.CatalogIndexFile + ".\n\n" +
			"Pipe it to a file and wire it into your editor via a yaml-language-server\n" +
			"modeline at the top of the file:\n\n" +
			fmt.Sprintf("  %s schema > manifest.schema.json\n", brand.Name) +
			"  # then add this as the first line of services/<name>.yaml:\n" +
			"  # yaml-language-server: $schema=./manifest.schema.json\n\n" +
			fmt.Sprintf("  %s schema catalog > catalog.schema.json   # for %s\n\n", brand.Name, manifest.CatalogIndexFile) +
			"The manifest schema describes the PORTABLE manifest shape only — base_url and\n" +
			"secret refs bind in profile.yaml, not in a manifest.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"manifest", "catalog"},
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "schema"
			kind := "manifest"
			if len(args) == 1 {
				kind = args[0]
			}
			switch kind {
			case "manifest":
				_, _ = r.stdout.Write(schema.Manifest)
			case "catalog":
				_, _ = r.stdout.Write(schema.Catalog)
			default:
				return agentsafety.NewUsageError(fmt.Sprintf("unknown schema %q: want 'manifest' or 'catalog'", kind))
			}
			return nil
		},
	}
}
