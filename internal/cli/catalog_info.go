package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/spf13/cobra"
)

// cmdCatalogInfo prints one installed catalog's full detail: its identity (from
// the source's anyctl-catalog.yaml index, folded into the metadata at add time),
// its provenance (source/type/ref/pinned commit/timestamps), and the service
// names it provides. It is the marketplace-browsing verb — `catalog list` is the
// overview, this is the drill-in. Data → stdout; an unknown catalog is a usage
// error (exit 2).
func (r *runner) cmdCatalogInfo() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show one installed catalog's identity, provenance, and services",
		Long: "Print an installed catalog's full detail: its name, description, version, and\n" +
			"homepage (from the source's " + manifest.CatalogIndexFile + " index), its source\n" +
			"(and subdir), type, requested ref, pinned commit, added/updated timestamps,\n" +
			"and the service names it provides. Data goes to stdout; an unknown catalog is\n" +
			"a usage error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			return r.catalogInfo(args[0])
		},
	}
	return cmd
}

// catalogInfo renders one installed catalog's detail. It reads the provenance
// record (ReadCatalogMeta) and the service listing (CatalogServiceNames); an
// unknown catalog surfaces as a *ConfigError (exit 2) from either.
func (r *runner) catalogInfo(name string) error {
	configDir := r.configDir()
	meta, found, err := manifest.ReadCatalogMeta(configDir, name)
	if err != nil {
		return err
	}
	if !found {
		return agentsafety.NewUsageError(fmt.Sprintf("catalog %q is not installed", name))
	}
	services, err := manifest.CatalogServiceNames(configDir, name)
	if err != nil {
		return err
	}

	rows := []struct{ label, value string }{
		{"name", meta.Name},
		{"description", orDash(meta.Description)},
		{"version", orDash(meta.Version)},
		{"homepage", orDash(meta.Homepage)},
		{"source", orDash(sourceLabel(meta.Source, meta.Path))},
		{"type", orDash(meta.Type)},
		{"ref", orDash(meta.Ref)},
		{"commit", orDash(meta.Commit)},
		{"added", timestamp(meta.AddedAt)},
		{"updated", timestamp(meta.UpdatedAt)},
		{"services", servicesLine(services)},
	}
	for _, row := range rows {
		_, _ = fmt.Fprintf(r.stdout, "%-12s %s\n", row.label+":", row.value)
	}
	return nil
}

// orDash returns "-" for an empty field so every info row aligns and a missing
// value reads clearly instead of a blank.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// servicesLine renders the provided service names as a comma-separated list, or
// "-" when the catalog provides none.
func servicesLine(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ", ")
}

// timestamp renders a metadata time in RFC3339, or "-" for the zero value (an
// older or hand-placed catalog with no recorded timestamps).
func timestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
