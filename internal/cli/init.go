package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/spf13/cobra"
)

// defaultConfigYAML is the minimal config.yaml `anyctl init` provisions. It is a
// generic, portable header (no service-specific defaults) mirroring examples/full/config.yaml.
var defaultConfigYAML = fmt.Sprintf(`# %s global config. Every field is optional; these are the defaults.
# Unknown top-level keys are an error (strict decoding) — a typo is rejected at
# load time rather than silently ignored.
version: 1
defaults:
  timeout: 60s      # per-request HTTP timeout
  output: json      # json | raw | scalar

# Secrets. The zero-dependency default needs no provider at all: set a
# <PREFIX>_<SECRET> env var (env_override) and it is used verbatim, skipping
# resolution. Providers cover the rest, dispatched by a ref's URI scheme.
secrets:
  env_override: true                     # allow <PREFIX>_<SECRET> env to skip resolution
  providers:
    onepassword:                         # map key aliases to scheme: op
      scheme: op
      command: ["op", "read", "{ref}"]   # {ref} ← the op:// URI
    # A generic exec provider runs any {ref}-templated command (pass, vault, sops):
    # pass:
    #   type: exec
    #   command: ["pass", "show", "{ref}"]   # a pass://… ref → its stdout
    # A file provider reads a secret from an owner-only (0600/0400) file:
    # file:
    #   type: file                            # a file:///run/secrets/token ref

# The legacy single-resolver `+"`secret:`"+` block is a still-supported deprecated alias.
`, brand.Name)

// defaultProfileYAML is the commented profile.yaml stub `anyctl init` provisions.
// A profile binds portable manifests to THIS machine's endpoints and secret refs.
// It is the SOLE binding mechanism: a manifest carries the portable shape only —
// an in-manifest base_url or secret ref is rejected by `lint`.
var defaultProfileYAML = fmt.Sprintf(`# %s per-user profile: binds portable manifests to THIS machine's endpoints
# and credentials. This is the only place a base_url or secret ref may live — a
# manifest carries the portable shape only (an in-manifest base_url/ref is
# rejected). Precedence at resolution time: env override > profile.
version: 1
services:
  # Bind a portable manifest (services/<name>.yaml) to your machine here, e.g.:
  #
  # example:
  #   base_url: https://example.my-lan.example
  #   secrets:
  #     api_key:
  #       ref: "op://VAULT/ITEM/FIELD"
`, brand.Name)

// cmdInit has two modes. Bare ` + "`anyctl init`" + ` provisions the config dir
// (config.yaml + services/ + profile.yaml), creating only what is missing.
// ` + "`anyctl init <service>`" + ` scaffolds a portable starter manifest for a new
// service, printing to stdout by default or writing to --output (refusing to
// clobber unless --force).
func (r *runner) cmdInit() *cobra.Command {
	var auth string
	var outPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "init [service]",
		Short: "provision the config dir, or scaffold a new service manifest",
		Long: "With no argument, provision the config dir idempotently: create\n" +
			"config.yaml, services/, and a commented profile.yaml — leaving any that\n" +
			"already exist untouched.\n\n" +
			"With a <service> argument, emit a portable starter manifest that teaches\n" +
			"the schema (commands + auth strategy + secret slots); the machine-specific\n" +
			"base_url and secret refs go in profile.yaml (shown in a trailing comment).\n" +
			"It prints to stdout by default; use -o to write it to a file. The output\n" +
			fmt.Sprintf("validates cleanly (`%s lint <file>`).\n\n", brand.Name) +
			"To fork a service from an installed catalog, copy its manifest out of\n" +
			"<config-dir>/catalogs/<catalog>/<name>.yaml into services/<name>.yaml, where a\n" +
			"local file overrides the catalog's by name.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "init"
			if len(args) == 0 {
				return r.provisionConfigDir()
			}
			return r.scaffoldService(args[0], auth, outPath, force)
		},
	}
	cmd.Flags().StringVar(&auth, "auth", manifest.DefaultScaffoldAuth,
		"auth scheme for the stanza: "+strings.Join(manifest.ScaffoldAuthSchemes, "|"))
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "write the template to a file instead of stdout")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the output file if it already exists")
	return cmd
}

// scaffoldService emits a portable starter manifest for name to stdout or a file.
func (r *runner) scaffoldService(name, auth, outPath string, force bool) error {
	tmpl, err := manifest.Scaffold(name, auth)
	if err != nil {
		return agentsafety.NewUsageError(err.Error())
	}
	if outPath == "" {
		_, _ = fmt.Fprint(r.stdout, tmpl)
		return nil
	}
	if !force {
		if _, statErr := os.Stat(outPath); statErr == nil {
			return agentsafety.NewUsageError(fmt.Sprintf("%s already exists; pass --force to overwrite", outPath))
		}
	}
	if err := os.WriteFile(outPath, []byte(tmpl), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}
	_, _ = fmt.Fprintf(r.stderr, "wrote %s\n", outPath)
	return nil
}

// provisionConfigDir creates the config dir scaffold idempotently: the dir,
// services/, a minimal config.yaml, and a commented profile.yaml. It clobbers
// nothing that already exists and prints one line per action to stderr (stdout
// stays data-only). Honors --config-dir / ANYCTL_CONFIG_DIR.
func (r *runner) provisionConfigDir() error {
	dir := r.configDir()
	if err := r.ensureDir(dir); err != nil {
		return err
	}
	if err := r.ensureDir(filepath.Join(dir, "services")); err != nil {
		return err
	}
	if err := r.ensureFile(filepath.Join(dir, "config.yaml"), defaultConfigYAML); err != nil {
		return err
	}
	if err := r.ensureFile(filepath.Join(dir, "profile.yaml"), defaultProfileYAML); err != nil {
		return err
	}
	// Point the user at the next concrete step: bind one service, then verify
	// just that service (the scoped strict lint), rather than run a bare
	// `lint --strict` that would flag every still-unbound service.
	_, _ = fmt.Fprintf(r.stderr,
		"\nNext: bind a service in %s, then run `%s lint --strict <service>` to verify it.\n",
		filepath.Join(dir, "profile.yaml"), brand.Name)
	return nil
}

// configDir resolves the config dir the same way Load does, honoring the
// --config-dir flag, then the loaded dir, then the XDG/env default.
func (r *runner) configDir() string {
	if r.flags.configDir != "" {
		return r.flags.configDir
	}
	if r.loaded != nil && r.loaded.Dir != "" {
		return r.loaded.Dir
	}
	return manifest.ConfigDir()
}

// ensureDir creates dir (and parents) if absent, reporting which case held.
func (r *runner) ensureDir(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", dir)
		}
		_, _ = fmt.Fprintf(r.stderr, "exists, left as-is: %s/\n", dir)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	_, _ = fmt.Fprintf(r.stderr, "created: %s/\n", dir)
	return nil
}

// ensureFile writes content to path only if the file is absent, reporting which
// case held. An existing file is never clobbered.
func (r *runner) ensureFile(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		_, _ = fmt.Fprintf(r.stderr, "exists, left as-is: %s\n", path)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	_, _ = fmt.Fprintf(r.stderr, "created: %s\n", path)
	return nil
}
