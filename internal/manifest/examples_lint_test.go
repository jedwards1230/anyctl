package manifest

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// examplesDir resolves the repo's full example config dir relative to this test
// file's package directory (internal/manifest → ../../examples/full).
func examplesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples", "full"))
	if err != nil {
		t.Fatalf("resolve examples dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.yaml")); err != nil {
		t.Fatalf("examples/full/profile.yaml not found at %s: %v", dir, err)
	}
	return dir
}

// TestExamplesLoadAndValidate turns the shipped examples/full config into a living
// contract. examples/full is the post-floor reference: an INSTALLED catalog under
// catalogs/reference/ (the two generic manifests) plus a profile.yaml that binds
// both services. This proves the honest model — anyctl ships no built-in services,
// so an installed catalog + a profile is what makes a complete, working config.
//
// It performs no network calls and resolves no secrets — Load is purely
// structural (YAML parse + Validate + ValidateConfig + offline spec inference).
func TestExamplesLoadAndValidate(t *testing.T) {
	dir := examplesDir(t)

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil Loaded")
	}

	// The loaded config itself must validate (Load already checks this, but the
	// example set is a contract — assert it explicitly so a future change that
	// loosens Load can't silently ship an invalid example config).
	if err := ValidateConfig(&loaded.Config); err != nil {
		t.Fatalf("ValidateConfig(examples/full/config.yaml): %v", err)
	}

	// examples/full installs the generic reference catalog and binds it in
	// profile.yaml — exactly the two services below, each from catalog:reference.
	want := []string{"inventory", "uptime"}
	got := loaded.CanonicalNames()
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("CanonicalNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CanonicalNames() = %v, want %v", got, want)
		}
	}

	for _, name := range want {
		svc, ok := loaded.Services[name]
		if !ok {
			t.Errorf("service %q did not register", name)
			continue
		}
		if origin := loaded.OriginOf(name); origin != catalogOrigin("reference") {
			t.Errorf("%s origin = %q, want %q", name, origin, catalogOrigin("reference"))
		}
		t.Run(name, func(t *testing.T) {
			// Structural Validate already ran on the RAW manifest during Load; it
			// cannot be re-run on `svc` here because the loaded service has been
			// profile-merged and now carries base_url/refs, which the structural
			// "no in-manifest binding" rule forbids. Load applies
			// examples/full/profile.yaml, so the installed catalog must be COMPLETE
			// through it: each portable manifest (no base_url or secret ref) is
			// bound to its endpoint and credentials via the profile. This proves the
			// installed-catalog + profile are a working end-to-end config.
			if err := ValidateComplete(svc); err != nil {
				t.Fatalf("ValidateComplete(%s): %v", name, err)
			}
		})
	}
}
