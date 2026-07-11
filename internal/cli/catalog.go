package cli

import (
	"github.com/spf13/cobra"
)

// cmdCatalog manages INSTALLED (named) catalogs: bundles of portable manifests
// installed from a directory or git repo into <config-dir>/catalogs/<name>/.
// anyctl ships no built-in services, so an installed catalog (or a local
// services/<name>.yaml) is the only way to give the binary something to run.
//
// A local services/<name>.yaml overrides an installed-catalog service of the same
// name; two installed catalogs may share a name, in which case the bare name is
// ambiguous and each stays addressable as "<catalog>:<service>".
func (r *runner) cmdCatalog() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Install and manage named service-manifest catalogs",
		Long: "Install and manage named catalogs — bundles of portable service manifests\n" +
			"from a directory or git repo, installed into <config-dir>/catalogs/<name>/.\n\n" +
			"Git is the first-class distribution path: publish a catalog by pushing a repo\n" +
			"with an anyctl-catalog.yaml index plus its manifests (GitHub, Forgejo,\n" +
			"Codeberg, GitLab, any git host), and install it with `catalog add <url>`.\n\n" +
			"anyctl ships with no built-in services: an installed catalog (or a local\n" +
			"services/<name>.yaml) is what gives the binary something to run. A local\n" +
			"file overrides an installed-catalog service of the same name; two installed\n" +
			"catalogs may share a name, and each stays addressable as <catalog>:<service>.\n\n" +
			"To author a NEW manifest, scaffold one with `anyctl init` into services/ and\n" +
			"validate it with `anyctl lint`.",
	}
	cmd.AddCommand(r.cmdCatalogAdd())
	cmd.AddCommand(r.cmdCatalogUpdate())
	cmd.AddCommand(r.cmdCatalogRemove())
	cmd.AddCommand(r.cmdCatalogList())
	cmd.AddCommand(r.cmdCatalogInfo())
	cmd.AddCommand(r.cmdCatalogValidate())
	return cmd
}
