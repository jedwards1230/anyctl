package manifest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jedwards1230/labctl/internal/manifest"
)

// TestScaffoldValidates writes the init output for every supported --auth scheme
// to a temp file and loads/validates it, asserting no error. This is the
// guarantee that `labctl init` output passes `labctl lint`.
func TestScaffoldValidates(t *testing.T) {
	dir := t.TempDir()
	for _, auth := range manifest.ScaffoldAuthSchemes {
		t.Run(auth, func(t *testing.T) {
			out, err := manifest.Scaffold("demo", auth)
			if err != nil {
				t.Fatalf("Scaffold(%q): %v", auth, err)
			}
			path := filepath.Join(dir, auth+".yaml")
			if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
				t.Fatal(err)
			}
			svc, err := manifest.LoadService(path, manifest.Config{})
			if err != nil {
				t.Fatalf("LoadService(%q output): %v\n---\n%s", auth, err, out)
			}
			if svc.Name != "demo" {
				t.Errorf("name = %q, want demo", svc.Name)
			}
			if err := manifest.Validate(svc); err != nil {
				t.Fatalf("Validate(%q output): %v", auth, err)
			}
			// The scaffold must stay generic — no homelab specifics leak in.
			if strings.Contains(out, "lilbro.cloud") {
				t.Errorf("%q scaffold contains a lilbro.cloud URL", auth)
			}
			if strings.Contains(out, "op://homelab") {
				t.Errorf("%q scaffold contains an op://homelab ref", auth)
			}
		})
	}
}

// TestScaffoldIsPortable confirms the scaffolded manifest omits the
// user-specific base_url/tls_insecure and secret refs (those move to
// profile.yaml) while still validating structurally and carrying a commented
// profile.yaml entry.
func TestScaffoldIsPortable(t *testing.T) {
	out, err := manifest.Scaffold("demo", "header-key")
	if err != nil {
		t.Fatal(err)
	}
	// The ACTIVE (non-comment) manifest lines must NOT carry base_url or a secret
	// ref — those move to profile.yaml. Comment lines (the profile section) may.
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "base_url:") {
			t.Errorf("portable scaffold has an active base_url line: %q", line)
		}
		if strings.HasPrefix(trimmed, "ref:") {
			t.Errorf("portable scaffold has an active secret ref line: %q", line)
		}
	}
	// It must teach the profile.yaml binding in a commented section.
	if !strings.Contains(out, "profile.yaml") {
		t.Errorf("scaffold should reference profile.yaml:\n%s", out)
	}
	if !strings.Contains(out, `ref: "op://VAULT/ITEM/FIELD"`) {
		t.Errorf("scaffold profile section should show a commented secret ref:\n%s", out)
	}
}

// TestScaffoldProfileEntry confirms the active profile entry carries the
// machine-specific base_url and a ref for each declared secret.
func TestScaffoldProfileEntry(t *testing.T) {
	entry, err := manifest.ScaffoldProfileEntry("demo", "header-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  demo:", "base_url: https://demo.example.com", "api_key:", "ref: \"op://VAULT/ITEM/FIELD\""} {
		if !strings.Contains(entry, want) {
			t.Errorf("profile entry missing %q:\n%s", want, entry)
		}
	}
	// ws-login adds tls_insecure and a wss base_url.
	ws, err := manifest.ScaffoldProfileEntry("demo", "ws-login")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ws, "tls_insecure: true") || !strings.Contains(ws, "base_url: wss://demo.example.com/api") {
		t.Errorf("ws-login profile entry should set wss base_url + tls_insecure:\n%s", ws)
	}
	// An unknown auth scheme is an error.
	if _, err := manifest.ScaffoldProfileEntry("demo", "magic"); err == nil {
		t.Fatal("unknown auth scheme should error")
	}
}

// TestScaffoldDefaultAuth confirms an empty auth defaults to header-key.
func TestScaffoldDefaultAuth(t *testing.T) {
	out, err := manifest.Scaffold("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "strategy: header-key") {
		t.Errorf("default scaffold should use header-key strategy:\n%s", out)
	}
}

// TestScaffoldTokenIsBearer confirms the token alias emits a bearer strategy
// with scheme: token (there is no standalone "token" manifest strategy).
func TestScaffoldTokenIsBearer(t *testing.T) {
	out, err := manifest.Scaffold("demo", "token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "strategy: bearer") || !strings.Contains(out, "scheme: token") {
		t.Errorf("token scaffold should be bearer + scheme: token:\n%s", out)
	}
}

// TestScaffoldUnknownAuth confirms an unknown scheme is rejected.
func TestScaffoldUnknownAuth(t *testing.T) {
	if _, err := manifest.Scaffold("demo", "magic"); err == nil {
		t.Fatal("unknown auth scheme should error")
	}
}

// TestEnvPrefix exercises the env-prefix derivation for names with separators.
func TestScaffoldEnvPrefix(t *testing.T) {
	out, err := manifest.Scaffold("my-cool.svc", "header-key")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "env_prefix: MY_COOL_SVC") {
		t.Errorf("env_prefix should collapse separators to underscores:\n%s", out)
	}
}
