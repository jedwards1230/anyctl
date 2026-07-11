package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/spf13/cobra"
)

// A named catalog is an installable bundle of portable manifests under
// <config-dir>/catalogs/<name>/. It only makes more manifests AVAILABLE — every
// manifest is validated on add to be portable (no base_url, no secret ref), so an
// installed catalog is inert until profile.yaml binds it. Precedence at load is
// local services/ > installed catalogs (there is no built-in floor). There is no
// execution-time gating here: anyctl stays an unopinionated executor.

// gitURLSchemes are the transport schemes permitted for a git source URL. ext/fd
// transport helpers (which can execute arbitrary commands) are excluded by both
// this allow-list and the GIT_ALLOW_PROTOCOL env passed to git.
var gitURLSchemes = []string{"https://", "http://", "ssh://", "git://", "file://"}

// scpStyleURL matches a scp-style git remote (user@host:path).
var scpStyleURL = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:.+$`)

// gitRefPattern restricts --ref to a safe ref-ish token (no leading '-', no shell
// metacharacters) so it can never be read as a git option or injected.
var gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func (r *runner) cmdCatalogAdd() *cobra.Command {
	var name, ref, path string
	var force, openapi bool
	cmd := &cobra.Command{
		Use:   "add <source> [--name <name>] [--ref <ref>] [--path <subdir>] [--force] [--openapi]",
		Short: "Install a named catalog from a dir, git URL, or OpenAPI document",
		Long: "Install a named catalog of portable manifests under\n" +
			"<config-dir>/catalogs/<name>/. <source> is either an existing local directory,\n" +
			"a git URL (https/http/ssh/git/file:// or scp-style user@host:path), or — with\n" +
			"--openapi — an OpenAPI 3.x document (an http(s):// URL or a local file).\n\n" +
			"A dir/git source MUST carry an " + manifest.CatalogIndexFile + " index file at its\n" +
			"root (the --path subdir for a subdir install). The index names and describes\n" +
			"the catalog and, optionally, curates its member manifests. Every member is\n" +
			"validated to be a PORTABLE manifest — no base_url, no secret ref — before\n" +
			"anything is written; one bad manifest rejects the whole add. A git source is\n" +
			"pinned to the resolved commit SHA, so an installed catalog is a reproducible,\n" +
			"inert bundle until profile.yaml binds it.\n\n" +
			"With no `manifests:` list in the index, every top-level *.yaml/*.yml (except\n" +
			"the index) is a member; with one, exactly those files install, in that order.\n\n" +
			"--path installs from a subdirectory of a git repo instead of its root — for a\n" +
			"repo that keeps its catalog under, say, anyctl-catalog/. The subdir is recorded\n" +
			"so `catalog update` re-fetches from the same place. It must be a relative path\n" +
			"within the clone (no absolute path, no `..` escaping the repo) and is git-only.\n\n" +
			"--openapi materializes a single-service portable manifest from the document:\n" +
			"its operations become commands: and its security schemes are inferred into an\n" +
			"auth: block on a best-effort basis (anything that can't be faithfully mapped\n" +
			"falls back to `auth: { strategy: none }` with a comment explaining what to wire\n" +
			"by hand). The spec is parsed once at add-time; it is NOT vendored and no spec:\n" +
			"reference is kept — the installed manifest stands alone. An --openapi source\n" +
			"needs no index (anyctl synthesizes the manifest); --ref and --path do not apply\n" +
			"to it (they are git-only).\n\n" +
			"The catalog name is --name, else the index `name` (else, with --openapi, the\n" +
			"document's info.title, slugified). --ref selects a git branch/tag/commit.\n" +
			"--force replaces an already-installed catalog of the same name.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			if openapi {
				if ref != "" {
					return agentsafety.NewUsageError("--ref is git-only and cannot be combined with --openapi")
				}
				if path != "" {
					return agentsafety.NewUsageError("--path is git-only and cannot be combined with --openapi")
				}
				return r.catalogAddOpenAPI(args[0], name, force)
			}
			return r.catalogAdd(args[0], name, ref, path, force)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "catalog name (default: the index `name`, or the OpenAPI document's title with --openapi)")
	cmd.Flags().StringVar(&ref, "ref", "", "git branch, tag, or commit to check out (git sources only)")
	cmd.Flags().StringVar(&path, "path", "", "subdirectory within a git repo to install manifests from (git sources only)")
	cmd.Flags().BoolVar(&force, "force", false, "replace an already-installed catalog of the same name")
	cmd.Flags().BoolVar(&openapi, "openapi", false, "treat <source> as an OpenAPI 3.x document (http(s):// URL or local file) and materialize a manifest from it")
	return cmd
}

func (r *runner) cmdCatalogUpdate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Re-fetch installed catalogs from their recorded source (all if none named)",
		Long: "Re-fetch one or more installed catalogs (or every installed catalog when no\n" +
			"name is given) from their recorded source, re-reading each source's\n" +
			manifest.CatalogIndexFile + " index and re-validating every member manifest as\n" +
			"portable (the same fail-closed gate as `catalog add`). A git source is\n" +
			"re-cloned at its recorded ref and re-pinned to the new commit SHA; the index's\n" +
			"description/version/homepage and member selection are refreshed too.\n" +
			"Per-catalog outcomes are reported to stderr; a failure on one catalog does not\n" +
			"abort the others.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			return r.catalogUpdate(args)
		},
	}
	return cmd
}

func (r *runner) cmdCatalogRemove() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Uninstall a named catalog",
		Long: "Uninstall a named catalog (delete <config-dir>/catalogs/<name>/). Its\n" +
			"services disappear from the next load (unless another installed catalog or a\n" +
			"local services/ file still defines the name). Removing a catalog that is not\n" +
			"installed is an error.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			return r.catalogRemove(args[0])
		},
	}
	return cmd
}

func (r *runner) cmdCatalogList() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed catalogs (name, version, services, pinned commit, source)",
		Long: "List every installed catalog, one per line: its name, index version (`-`\n" +
			"when the index carries none), the number of service manifests it provides,\n" +
			"the pinned git commit (short SHA, else the requested ref, else `-` for a dir\n" +
			"source), and its source. Data goes to stdout. Use `catalog info <name>` for\n" +
			"one catalog's full detail.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			return r.catalogList()
		},
	}
	return cmd
}

// catalogAdd installs a catalog from a dir or git source. Fetch → read the
// required anyctl-catalog.yaml index → validate every member manifest
// (fail-closed) → atomic install. Nothing is written unless the index is present
// and valid, every selected manifest is a valid portable manifest, and no
// service name collides within the source. The name is --name, else the index's
// `name` (basename inference is gone — the index is the identity record).
func (r *runner) catalogAdd(source, name, ref, path string, force bool) error {
	srcType, err := classifySource(source)
	if err != nil {
		return err
	}
	// A supplied --name must be a valid single path segment; an index-supplied
	// name is already validated by ParseCatalogIndex.
	if name != "" {
		if err := manifest.ValidateName(name); err != nil {
			return agentsafety.NewUsageError(err.Error())
		}
	}
	if ref != "" && srcType != "git" {
		return agentsafety.NewUsageError("--ref only applies to a git source")
	}
	if path != "" && srcType != "git" {
		return agentsafety.NewUsageError("--path only applies to a git source (point a local dir source directly at the subdirectory instead)")
	}

	tmp, err := os.MkdirTemp("", brand.Name+"-catalog-fetch-")
	if err != nil {
		return fmt.Errorf("creating tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	now := time.Now().UTC()
	meta := manifest.CatalogMeta{Source: source, Type: srcType, AddedAt: now, UpdatedAt: now}
	fetchDir := source
	label := source
	if srcType == "git" {
		commit, err := r.gitFetch(source, ref, tmp)
		if err != nil {
			return err
		}
		meta.Ref = ref
		meta.Commit = commit
		meta.Path = path
		fetchDir, err = resolveCatalogSubdir(tmp, path)
		if err != nil {
			return err
		}
		label = sourceLabel(source, path)
	}

	idx, err := manifest.ReadCatalogIndex(fetchDir)
	if err != nil {
		return r.catalogIndexError(label, err)
	}
	// Name precedence: --name flag wins over the index name.
	if name == "" {
		name = idx.Name
	}
	meta.Name = name
	meta.Description = idx.Description
	meta.Version = idx.Version
	meta.Homepage = idx.Homepage

	files, err := r.collectAndValidate(fetchDir, label, idx.Manifests)
	if err != nil {
		return err
	}
	if err := manifest.InstallCatalog(r.configDir(), meta, files, force); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(r.stderr, "installed catalog %q (%s) from %s%s\n", name, countManifests(len(files)), label, commitSuffix(meta.Commit))
	return nil
}

// catalogIndexError renders a missing-index failure as an actionable usage error
// naming the file and printing a 3-line starter snippet; any other index error
// (parse/schema/field, already a *ConfigError) passes through unchanged.
func (r *runner) catalogIndexError(source string, err error) error {
	if errors.Is(err, manifest.ErrCatalogIndexMissing) {
		return agentsafety.NewUsageError(fmt.Sprintf(
			"%s has no %s at its root — a catalog source must carry an index file. Add one with:\n\n"+
				"  name: my-catalog                # ^[a-z0-9][a-z0-9_-]*$; the default install name\n"+
				"  description: what this catalog provides\n"+
				"  # optional: version, homepage, and a curated `manifests:` list",
			source, manifest.CatalogIndexFile))
	}
	return err
}

// catalogUpdate re-fetches the named catalogs (or every installed catalog when
// names is empty). Each named catalog's name is validated up front. A per-catalog
// failure is reported and the first such error is returned, but it never aborts
// the rest.
func (r *runner) catalogUpdate(names []string) error {
	configDir := r.configDir()
	var targets []string
	if len(names) > 0 {
		for _, name := range names {
			if err := manifest.ValidateName(name); err != nil {
				return agentsafety.NewUsageError(err.Error())
			}
		}
		targets = names
	} else {
		cats, err := manifest.InstalledCatalogs(configDir)
		if err != nil {
			return err
		}
		for _, c := range cats {
			targets = append(targets, c.Name)
		}
		if len(targets) == 0 {
			_, _ = fmt.Fprintln(r.stderr, "no catalogs installed")
			return nil
		}
	}
	var firstErr error
	for _, t := range targets {
		if err := r.updateOne(configDir, t); err != nil {
			_, _ = fmt.Fprintf(r.stderr, "catalog %q: update failed: %v\n", t, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// updateOne re-fetches and re-installs a single catalog from its recorded source.
func (r *runner) updateOne(configDir, name string) error {
	meta, found, err := manifest.ReadCatalogMeta(configDir, name)
	if err != nil {
		return err
	}
	if !found {
		return agentsafety.NewUsageError(fmt.Sprintf("catalog %q is not installed", name))
	}
	if meta.Source == "" || meta.Type == "" {
		return fmt.Errorf("catalog %q has no recorded source; remove and re-add it", name)
	}
	// The install directory is the source of truth for the name — a hand-edited
	// lock file must not retarget the install to a different catalog.
	meta.Name = name
	meta.UpdatedAt = time.Now().UTC()

	if meta.Type == "openapi" {
		return r.updateOneOpenAPI(configDir, meta)
	}

	tmp, err := os.MkdirTemp("", brand.Name+"-catalog-fetch-")
	if err != nil {
		return fmt.Errorf("creating tempdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	fetchDir := meta.Source
	label := meta.Source
	switch meta.Type {
	case "dir":
		// re-read the source dir
	case "git":
		commit, err := r.gitFetch(meta.Source, meta.Ref, tmp)
		if err != nil {
			return err
		}
		meta.Commit = commit
		fetchDir, err = resolveCatalogSubdir(tmp, meta.Path)
		if err != nil {
			return err
		}
		label = sourceLabel(meta.Source, meta.Path)
	default:
		return fmt.Errorf("catalog %q has unknown source type %q", name, meta.Type)
	}

	// Re-read the source's index on every update: refresh the folded-in metadata
	// (description/version/homepage) and the member selection so a curated
	// `manifests:` list or a renamed description in the source is picked up. The
	// install NAME stays fixed (the install dir is the source of truth); only the
	// index's other fields flow through.
	idx, err := manifest.ReadCatalogIndex(fetchDir)
	if err != nil {
		return r.catalogIndexError(label, err)
	}
	meta.Description = idx.Description
	meta.Version = idx.Version
	meta.Homepage = idx.Homepage

	files, err := r.collectAndValidate(fetchDir, label, idx.Manifests)
	if err != nil {
		return err
	}
	if err := manifest.InstallCatalog(configDir, meta, files, true); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(r.stderr, "updated catalog %q (%s) from %s%s\n", name, countManifests(len(files)), label, commitSuffix(meta.Commit))
	return nil
}

// updateOneOpenAPI re-runs the same pipeline `catalog add --openapi` uses
// (fetch source → GenerateManifestFromSpec → ValidatePortableManifest) against
// meta.Source, re-installing the result as meta.Name.yaml. This keeps an
// openapi-sourced catalog symmetric with dir/git: re-fetch, re-validate
// fail-closed, re-install — picking up any upstream spec change. A
// local-file source that has since moved simply errors here (the fetch
// error), the same as a git remote going away.
func (r *runner) updateOneOpenAPI(configDir string, meta manifest.CatalogMeta) error {
	specBytes, err := fetchOpenAPISource(meta.Source)
	if err != nil {
		return err
	}
	data, err := manifest.GenerateManifestFromSpec(meta.Name, specBytes)
	if err != nil {
		return err
	}
	// GenerateManifestFromSpec already validates its own output; call the
	// install-time gate explicitly too, mirroring catalogAddOpenAPI.
	if _, err := manifest.ValidatePortableManifest(data); err != nil {
		return err
	}
	files := map[string][]byte{meta.Name + ".yaml": data}
	if err := manifest.InstallCatalog(configDir, meta, files, true); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(r.stderr, "updated catalog %q (1 manifest) from OpenAPI document %s\n", meta.Name, meta.Source)
	return nil
}

func (r *runner) catalogRemove(name string) error {
	if err := manifest.RemoveCatalog(r.configDir(), name); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(r.stderr, "removed catalog %q\n", name)
	return nil
}

// catalogList prints one row per installed catalog: NAME VERSION SERVICES PINNED
// SOURCE. VERSION is the index version (`-` if none); SERVICES is the count of
// service manifests in the installed dir; PINNED is the short commit SHA, else
// the requested ref, else `-`; SOURCE is where it came from. Data → stdout.
func (r *runner) catalogList() error {
	configDir := r.configDir()
	cats, err := manifest.InstalledCatalogs(configDir)
	if err != nil {
		return err
	}
	for _, c := range cats {
		ver := c.Version
		if ver == "" {
			ver = "-"
		}
		pinned := "-"
		if c.Commit != "" {
			pinned = shortSHA(c.Commit)
		} else if c.Ref != "" {
			pinned = c.Ref
		}
		src := c.Source
		if src == "" {
			src = "-"
		}
		services := 0
		if names, err := manifest.CatalogServiceNames(configDir, c.Name); err == nil {
			services = len(names)
		}
		_, _ = fmt.Fprintf(r.stdout, "%-16s %-10s %-8d %-14s %s\n", c.Name, ver, services, pinned, src)
	}
	return nil
}

// collectAndValidate validates a catalog source's member manifests (the index's
// `manifests:` list, or every top-level *.yaml/*.yml when members is nil) as
// portable manifests (fail-closed), rejecting a duplicate service name within the
// source. It returns the files keyed by base filename, ready for InstallCatalog.
// A service name already defined by ANOTHER installed catalog is allowed — both
// stay addressable via their qualified "<catalog>:<service>" selector; the bare
// name becomes ambiguous (see manifest.Loaded.Ambiguous) and is resolved at
// load/lookup time, not blocked here.
func (r *runner) collectAndValidate(fetchDir, source string, members []string) (map[string][]byte, error) {
	entries, err := validateEntries(fetchDir, members)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		switch {
		case e.readErr != nil:
			return nil, e.readErr
		case e.valErr != nil:
			return nil, fmt.Errorf("%s: %w", e.file, e.valErr)
		case e.dupOf != "":
			return nil, agentsafety.NewUsageError(fmt.Sprintf("source defines service %q twice (%s and %s)", e.name, e.dupOf, e.file))
		}
		files[e.file] = e.data
	}
	if len(files) == 0 {
		return nil, agentsafety.NewUsageError(fmt.Sprintf("no manifests (*.yaml) found in %s", source))
	}
	return files, nil
}

// gitFetch clones url into tmp (optionally checking out ref) using the system git,
// and returns the resolved HEAD commit SHA. The URL and ref are validated before
// any process runs; the URL is passed as a single arg after `--` (no shell), and
// ext/fd transport helpers are blocked both by the URL validation and by
// GIT_ALLOW_PROTOCOL in the subprocess env.
func (r *runner) gitFetch(url, ref, tmp string) (string, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", agentsafety.NewUsageError("git is required to add a git catalog source but was not found in PATH")
	}
	if err := validateGitURL(url); err != nil {
		return "", err
	}
	if ref != "" {
		if err := validateGitRef(ref); err != nil {
			return "", err
		}
	}
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https:http:ssh:git:file")
	run := func(args ...string) ([]byte, error) {
		c := exec.Command(gitBin, args...) // #nosec G204 -- argv built from a validated URL/ref, passed after --, no shell
		c.Env = env
		c.Stdin = nil
		var stderr bytes.Buffer
		c.Stderr = &stderr
		out, err := c.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
	if _, err := run("-c", "protocol.ext.allow=never", "-c", "protocol.fd.allow=never", "clone", "--quiet", "--", url, tmp); err != nil {
		return "", err
	}
	if ref != "" {
		// `--` separates the ref from any pathspec so a ref can never be read as an
		// option, matching the clone call above (ref is also pre-validated).
		if _, err := run("-C", tmp, "checkout", "--quiet", ref, "--"); err != nil {
			return "", err
		}
	}
	out, err := run("-C", tmp, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// classifySource decides whether source is a local dir or a git URL. An existing
// non-dir path, or a path that is neither an existing dir nor a plausible git URL,
// is a usage error.
func classifySource(source string) (string, error) {
	if source == "" {
		return "", agentsafety.NewUsageError("empty catalog source")
	}
	if info, err := os.Stat(source); err == nil {
		if !info.IsDir() {
			return "", agentsafety.NewUsageError(fmt.Sprintf("source %q is a file, not a directory or git URL", source))
		}
		return "dir", nil
	}
	if err := validateGitURL(source); err != nil {
		return "", err
	}
	return "git", nil
}

// resolveCatalogSubdir joins a validated relative subdir under the clone root and
// confirms it is an existing directory. An empty path returns root unchanged (the
// repo root is the catalog). It rejects an absolute path and any `..` that would
// escape the clone, then verifies (defense in depth) that the cleaned join stays
// under root — so a git source can never be made to read manifests from outside
// the fetched repo. A missing or non-directory target is a clear usage error.
func resolveCatalogSubdir(root, path string) (string, error) {
	if path == "" {
		return root, nil
	}
	if filepath.IsAbs(path) {
		return "", agentsafety.NewUsageError(fmt.Sprintf("--path %q must be a relative path within the repository, not absolute", path))
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", agentsafety.NewUsageError(fmt.Sprintf("--path %q escapes the repository root", path))
	}
	dest := filepath.Join(root, clean)
	if dest != root && !strings.HasPrefix(dest, root+string(filepath.Separator)) {
		return "", agentsafety.NewUsageError(fmt.Sprintf("--path %q escapes the repository root", path))
	}
	info, err := os.Stat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return "", agentsafety.NewUsageError(fmt.Sprintf("subdirectory %q not found in the repository", path))
		}
		return "", err
	}
	if !info.IsDir() {
		return "", agentsafety.NewUsageError(fmt.Sprintf("--path %q is a file, not a directory", path))
	}
	return dest, nil
}

// sourceLabel renders a "<source> (subdir <path>)" label for confirmation and
// error messages when a git source installs from a subdirectory, or just the
// source when it does not.
func sourceLabel(source, path string) string {
	if path == "" {
		return source
	}
	return fmt.Sprintf("%s (subdir %s)", source, path)
}

// validateGitURL allows only the safe transport schemes and scp-style remotes,
// and rejects anything that could invoke a transport helper (scheme::) or be read
// as a git option (leading '-').
func validateGitURL(url string) error {
	if url == "" {
		return agentsafety.NewUsageError("empty git URL")
	}
	if strings.HasPrefix(url, "-") {
		return agentsafety.NewUsageError(fmt.Sprintf("invalid git URL %q: must not start with '-'", url))
	}
	// Reject anything containing "::" — git reads scheme::path as a transport
	// helper (ext::/fd:: can run arbitrary commands). This also rejects a bare IPv6
	// literal, but those aren't valid git remotes without a scheme anyway, so the
	// over-broad guard is the safe tradeoff here.
	if strings.Contains(url, "::") {
		return agentsafety.NewUsageError(fmt.Sprintf("invalid git URL %q: transport helpers (scheme::) are not allowed", url))
	}
	for _, scheme := range gitURLSchemes {
		if strings.HasPrefix(url, scheme) {
			return nil
		}
	}
	if scpStyleURL.MatchString(url) {
		return nil
	}
	return agentsafety.NewUsageError(fmt.Sprintf("source %q is neither an existing directory nor a valid git URL (want https/http/ssh/git/file:// or user@host:path)", url))
}

// validateGitRef restricts --ref to a safe ref-ish token.
func validateGitRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return agentsafety.NewUsageError(fmt.Sprintf("invalid --ref %q: must not start with '-'", ref))
	}
	if !gitRefPattern.MatchString(ref) {
		return agentsafety.NewUsageError(fmt.Sprintf("invalid --ref %q: must match ^[A-Za-z0-9._/-]+$", ref))
	}
	return nil
}

// commitSuffix renders a "@<short-sha>" suffix for confirmation lines, or "" when
// there is no commit (a dir source).
func commitSuffix(commit string) string {
	if commit == "" {
		return ""
	}
	return "@" + shortSHA(commit)
}

// shortSHA truncates a git SHA to 12 chars for display.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// isYAMLFile reports whether name has a .yaml/.yml extension.
func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// countManifests renders a correctly-pluralized "N manifest(s)" phrase.
func countManifests(n int) string {
	if n == 1 {
		return "1 manifest"
	}
	return fmt.Sprintf("%d manifests", n)
}
