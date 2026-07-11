package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// TestCatalogValidateValid: a directory with one valid portable manifest
// validates clean (exit 0) and reports it "ok" on stdout. No ANYCTL_CONFIG_DIR
// is set — validate is config-dir-free.
func TestCatalogValidateValid(t *testing.T) {
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)
	writeCatalogIndex(t, dir, "ref")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("ok   widget.yaml (widget)")) {
		t.Errorf("stdout = %q, want an \"ok widget.yaml (widget)\" line", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("index anyctl-catalog.yaml: name=ref")) {
		t.Errorf("stdout = %q, want the index line", out.String())
	}
}

// TestCatalogValidateMissingIndex: a directory with manifests but no
// anyctl-catalog.yaml index fails validation naming the missing index file.
func TestCatalogValidateMissingIndex(t *testing.T) {
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("anyctl-catalog.yaml")) {
		t.Errorf("stderr = %q, want it to name the missing index file", errb.String())
	}
}

// TestCatalogValidateBadIndex: a present-but-invalid index (missing the required
// description) fails validation.
func TestCatalogValidateBadIndex(t *testing.T) {
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)
	if err := os.WriteFile(filepath.Join(dir, "anyctl-catalog.yaml"), []byte("name: ref\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
}

// TestCatalogValidateRejectsBinding: a manifest carrying a base_url or secret
// ref is non-portable — validate exits 2 (usage) and reports the failure.
func TestCatalogValidateRejectsBinding(t *testing.T) {
	for name, body := range map[string]string{
		"base_url":   "name: bound\nbase_url: https://h.example\nauth: { strategy: none }\n",
		"secret-ref": "name: bound\nsecrets:\n  token: { ref: op://v/i/f }\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSourceManifest(t, dir, "bound.yaml", body)
			writeCatalogIndex(t, dir, "ref")

			var out, errb bytes.Buffer
			if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
				t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
			}
			if !bytes.Contains(out.Bytes(), []byte("FAIL bound.yaml")) {
				t.Errorf("stdout = %q, want a \"FAIL bound.yaml\" line", out.String())
			}
		})
	}
}

// TestCatalogValidateDuplicateName: two manifests in the same directory
// defining the same service name is rejected (exit 2).
func TestCatalogValidateDuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)
	writeSourceManifest(t, dir, "widget2.yaml", portableWidget) // same `name: widget`
	writeCatalogIndex(t, dir, "ref")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("duplicate service name")) {
		t.Errorf("stdout = %q, want a duplicate-service-name diagnostic", out.String())
	}
}

// TestCatalogValidateEmptyDir: a directory with an index but no manifests is a
// usage error ('no manifests').
func TestCatalogValidateEmptyDir(t *testing.T) {
	dir := t.TempDir()
	writeCatalogIndex(t, dir, "ref")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("no manifests")) {
		t.Errorf("stderr = %q, want a 'no manifests' diagnostic", errb.String())
	}
}

// TestCatalogValidateMixedResults: one valid + one invalid manifest reports
// both lines (ok for the good one, FAIL for the bad one) and exits 2.
func TestCatalogValidateMixedResults(t *testing.T) {
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)
	writeSourceManifest(t, dir, "bad.yaml", "name: bad\nbogus_key: 1\nauth: { strategy: none }\n")
	writeCatalogIndex(t, dir, "ref")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("ok   widget.yaml")) {
		t.Errorf("stdout = %q, want the valid manifest reported ok despite the other failing", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("FAIL bad.yaml")) {
		t.Errorf("stdout = %q, want bad.yaml reported FAIL", out.String())
	}
}

// TestCatalogValidateNoConfigDirNeeded: validate works with no config dir set
// up at all (it never touches XDG/ANYCTL_CONFIG_DIR), confirming it is a
// standalone, config-dir-free check.
func TestCatalogValidateNoConfigDirNeeded(t *testing.T) {
	t.Setenv("ANYCTL_CONFIG_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
	dir := t.TempDir()
	writeSourceManifest(t, dir, "widget.yaml", portableWidget)
	writeCatalogIndex(t, dir, "ref")

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "validate", dir}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
}
