package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
)

// TestListShowsOverrideMarker: with a catalog installed, a local
// services/<name>.yaml that shadows one of its services is marked `override` in
// `list`, while an untouched catalog service stays `catalog:<name>`. The
// `override` origin needs an installed base to shadow (there is no embedded
// floor), so the test stages a catalog first.
func TestListShowsOverrideMarker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANYCTL_CONFIG_DIR", dir)

	// Install a two-service catalog (radarr + sonarr) from a source dir.
	src := filepath.Join(t.TempDir(), "mycat")
	writeSourceManifest(t, src, "radarr.yaml", svcManifest)
	writeSourceManifest(t, src, "sonarr.yaml", strings.Replace(svcManifest, "name: radarr", "name: sonarr", 1))
	writeCatalogIndex(t, src, "mycat")
	var out, errb bytes.Buffer
	if code := Run([]string{"catalog", "add", src}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("catalog add exit = %d, want 0 (stderr: %s)", code, errb.String())
	}

	// A local radarr manifest shadows the installed-catalog radarr.
	writeService(t, dir, "radarr", svcManifest)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"list"}, &out, &errb); code != agentsafety.ExitOK {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb.String())
	}
	got := out.String()
	for _, line := range strings.Split(got, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "radarr":
			if fields[1] != "override" {
				t.Errorf("radarr marker = %q, want override (line %q)", fields[1], line)
			}
		case "sonarr":
			if fields[1] != "catalog:mycat" {
				t.Errorf("sonarr marker = %q, want catalog:mycat (line %q)", fields[1], line)
			}
		}
	}
}
