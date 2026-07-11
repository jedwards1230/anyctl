package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/spf13/cobra"
)

// cmdCatalogValidate is a read-only check for a community catalog repository:
// it validates every top-level manifest in a directory against the exact gate
// `catalog add` enforces, with no network access, no config dir, and no
// install/profile/cross-catalog interaction. It is what a third-party catalog
// repo (and the validate-catalog GitHub Action) runs in CI to confirm its
// manifests satisfy anyctl's portable-manifest contract before anyone installs
// them with `anyctl catalog add`.
func (r *runner) cmdCatalogValidate() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <dir>",
		Short: "Validate a catalog source: its index and every member manifest (read-only)",
		Long: "Validate a catalog source directory — the same fail-closed gate `catalog add`\n" +
			"runs. First <dir> must carry an " + manifest.CatalogIndexFile + " index at its\n" +
			"root (present, schema-valid, well-formed name/description/members); then every\n" +
			"member manifest (the index's `manifests:` list, or every top-level *.yaml/*.yml\n" +
			"when it has none) is validated as a PORTABLE manifest — JSON Schema, then\n" +
			"structural Validate, which rejects an in-manifest base_url or secret ref. A\n" +
			"duplicate service name across two member manifests is also rejected.\n\n" +
			"This command is read-only: no network call, no install, no profile binding,\n" +
			"and no interaction with any installed catalog — it only inspects\n" +
			"the files in <dir>. That makes it the check a third-party catalog repository\n" +
			"runs in its own CI (see .github/actions/validate-catalog) to confirm its\n" +
			fmt.Sprintf("catalog satisfies %s's contract before anyone runs `%s catalog add`\n", brand.Name, brand.Name) +
			"against the repo.\n\n" +
			"Prints the index name, then one line per manifest (\"ok\" or \"FAIL\" with the\n" +
			"reason), and exits 0 only if the index and every manifest are valid.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curCommand = "catalog"
			return r.catalogValidate(args[0])
		},
	}
	return cmd
}

// catalogValidateResult is one manifest file's validation outcome.
type catalogValidateResult struct {
	file string
	name string
	err  error
}

// catalogValidate validates a catalog source dir: first its required
// anyctl-catalog.yaml index (present, schema-valid, well-formed members), then
// every selected member manifest, printing a per-file result to stdout. It
// shares the read → ValidatePortableManifest → stem-fallback →
// duplicate-detection walk with collectAndValidate (via validateEntries) but,
// unlike that fail-fast `catalog add` path, it reports every file so a CI run
// surfaces every problem in one pass. A missing index is the same fail-closed
// gate `catalog add` runs, with the same starter-snippet hint.
func (r *runner) catalogValidate(dir string) error {
	idx, err := manifest.ReadCatalogIndex(dir)
	if err != nil {
		return r.catalogIndexError(dir, err)
	}
	_, _ = fmt.Fprintf(r.stdout, "index %s: name=%s\n", manifest.CatalogIndexFile, idx.Name)

	entries, err := validateEntries(dir, idx.Manifests)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) == 0 {
		return agentsafety.NewUsageError(fmt.Sprintf("no manifests (*.yaml/*.yml) found in %s", dir))
	}

	results := make([]catalogValidateResult, 0, len(entries))
	for _, e := range entries {
		res := catalogValidateResult{file: e.file, name: e.name}
		switch {
		case e.readErr != nil:
			res.err = e.readErr
		case e.valErr != nil:
			res.err = e.valErr
		case e.dupOf != "":
			res.err = fmt.Errorf("duplicate service name %q (already defined by %s)", e.name, e.dupOf)
		}
		results = append(results, res)
	}

	var failed int
	for _, res := range results {
		if res.err != nil {
			failed++
			_, _ = fmt.Fprintf(r.stdout, "FAIL %s: %v\n", res.file, res.err)
			continue
		}
		_, _ = fmt.Fprintf(r.stdout, "ok   %s (%s)\n", res.file, res.name)
	}
	if failed > 0 {
		return agentsafety.NewUsageError(fmt.Sprintf("%d of %d manifest(s) failed validation in %s", failed, len(results), dir))
	}
	return nil
}

// manifestEntry is one manifest file's outcome from validateEntries: the shared
// read → ValidatePortableManifest → stem-fallback → duplicate-detection walk,
// with the caller-specific error formatting/typing left to the reducer. Exactly
// one of readErr/valErr is set when the file failed; dupOf names an earlier file
// in the same dir that already defined `name`; otherwise `data` holds the bytes.
type manifestEntry struct {
	file    string // base filename
	name    string // resolved service name (filename stem when the manifest is unnamed)
	data    []byte // file bytes (only meaningful for a valid, non-duplicate entry)
	readErr error  // file read failure, pre-formatted as "read <path>: ..."
	valErr  error  // ValidatePortableManifest failure (raw, unwrapped)
	dupOf   string // earlier file that defined `name`, when this entry duplicates it
}

// validateEntries walks a catalog source's member manifests (sorted for the
// glob case, index-declared order for the explicit case) and validates each as a
// portable manifest, returning one manifestEntry per file. members selects the
// mechanism: nil means auto-glob every top-level *.yaml/*.yml (except the index
// file itself); a non-nil list is the exact ordered set from the index's
// `manifests:`. It never fails fast and never wraps errors into a caller-specific
// type, so both the fail-fast `catalog add` path (collectAndValidate) and the
// accumulate-all `catalog validate` path reduce the identical walk differently.
// Only the dir-read error is returned directly (the callers wrap it with their
// own source/dir label).
func validateEntries(dir string, members []string) ([]manifestEntry, error) {
	files, err := memberFiles(dir, members)
	if err != nil {
		return nil, err
	}
	out := make([]manifestEntry, 0, len(files))
	svcToFile := map[string]string{} // service name → first file that defined it
	for _, fname := range files {
		me := manifestEntry{file: fname}
		path := filepath.Join(dir, fname)
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			me.readErr = fmt.Errorf("read %s: %w", path, readErr)
			out = append(out, me)
			continue
		}
		svcName, valErr := manifest.ValidatePortableManifest(b)
		if valErr != nil {
			me.valErr = valErr
			out = append(out, me)
			continue
		}
		if svcName == "" {
			svcName = strings.TrimSuffix(strings.TrimSuffix(fname, ".yaml"), ".yml")
		}
		me.name = svcName
		if prev, dup := svcToFile[svcName]; dup {
			me.dupOf = prev
			out = append(out, me)
			continue
		}
		svcToFile[svcName] = fname
		me.data = b
		out = append(out, me)
	}
	return out, nil
}

// memberFiles resolves the ordered member filenames to validate. With members
// nil it globs the top-level *.yaml/*.yml in dir (excluding the index file) in
// sorted order — today's discovery behavior. With members non-nil it returns
// exactly those filenames in the declared order; each is guarded as a bare path
// segment (defense in depth — ParseCatalogIndex already validated the shape) so
// a curated entry can never point outside the source. A listed file that is
// missing is left in the list so validateEntries surfaces it as a read error
// (fail-closed for `catalog add`, reported for `catalog validate`).
func memberFiles(dir string, members []string) ([]string, error) {
	if members != nil {
		out := make([]string, 0, len(members))
		for _, m := range members {
			if base := filepath.Base(m); base != m || base == "." || base == ".." {
				return nil, agentsafety.NewUsageError(fmt.Sprintf("invalid manifests entry %q: must be a bare file name", m))
			}
			out = append(out, m)
		}
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		// The index file is not a manifest; never glob it as a member.
		if e.Name() == manifest.CatalogIndexFile {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}
