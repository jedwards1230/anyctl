package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// TestCatalogInfo: `catalog info <name>` reports a catalog's index identity
// (name/description/version/homepage from the index), its dir provenance, and the
// service names it provides. Data goes to stdout.
func TestCatalogInfo(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)
	src := filepath.Join(t.TempDir(), "srcdir")
	writeSourceManifest(t, src, "widget.yaml", portableWidget)
	writeSourceManifest(t, src, "gadget.yaml", strings.Replace(portableWidget, "name: widget", "name: gadget", 1))
	// An index with version + homepage + curated members, so info can echo them.
	index := "name: mycat\ndescription: my test catalog\nversion: \"3.2.1\"\nhomepage: https://example.test/mycat\nmanifests:\n  - widget.yaml\n  - gadget.yaml\n"
	if err := os.WriteFile(filepath.Join(src, "anyctl-catalog.yaml"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("add exit = %d (stderr: %s)", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "info", "mycat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("info exit = %d (stderr: %s)", code, errb.String())
	}
	got := out.String()
	for _, want := range []string{"mycat", "my test catalog", "3.2.1", "https://example.test/mycat", "widget", "gadget"} {
		if !strings.Contains(got, want) {
			t.Errorf("info output missing %q:\n%s", want, got)
		}
	}
}

// TestCatalogInfoUnknown: `catalog info` on a not-installed catalog is a usage
// error (exit 2).
func TestCatalogInfoUnknown(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)

	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "info", "nope"}, &out, &errb); code != agentsafety.ExitUsage {
		t.Fatalf("info exit = %d, want %d (usage) (stderr: %s)", code, agentsafety.ExitUsage, errb.String())
	}
}

// TestCatalogUpdateVariadic: `catalog update a b` updates several named catalogs
// in one call (variadic), refreshing each from its source.
func TestCatalogUpdateVariadic(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", cfg)

	srcA := filepath.Join(t.TempDir(), "asrc")
	writeSourceManifest(t, srcA, "widget.yaml", portableWidget)
	writeCatalogIndex(t, srcA, "acat")
	srcB := filepath.Join(t.TempDir(), "bsrc")
	writeSourceManifest(t, srcB, "gadget.yaml", strings.Replace(portableWidget, "name: widget", "name: gadget", 1))
	writeCatalogIndex(t, srcB, "bcat")

	var out, errb bytes.Buffer
	for _, src := range []string{srcA, srcB} {
		out.Reset()
		errb.Reset()
		if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
			t.Fatalf("add %s exit = %d (stderr: %s)", src, code, errb.String())
		}
	}

	// Change both sources.
	writeSourceManifest(t, srcA, "widget.yaml", strings.Replace(portableWidget, "a widget", "an UPDATED widget", 1))
	writeSourceManifest(t, srcB, "gadget.yaml", strings.Replace(strings.Replace(portableWidget, "name: widget", "name: gadget", 1), "a widget", "an UPDATED gadget", 1))

	out.Reset()
	errb.Reset()
	if code := Run([]string{"catalog", "update", "acat", "bcat"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("variadic update exit = %d (stderr: %s)", code, errb.String())
	}
	for _, c := range []struct{ cat, file string }{{"acat", "widget.yaml"}, {"bcat", "gadget.yaml"}} {
		got, err := os.ReadFile(filepath.Join(cfg, "catalogs", c.cat, c.file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "UPDATED") {
			t.Errorf("catalog %s not refreshed by variadic update:\n%s", c.cat, got)
		}
	}
}
