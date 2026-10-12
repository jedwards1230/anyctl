package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// TestNormalizeSecretsLegacy proves the legacy secret: block is folded into a
// single op provider, and an empty config still yields a default op provider.
func TestNormalizeSecretsLegacy(t *testing.T) {
	t.Run("empty config synthesizes op provider", func(t *testing.T) {
		sc := NormalizeSecrets(Config{})
		p, ok := sc.Providers["onepassword"]
		if !ok {
			t.Fatalf("want an onepassword provider, got %v", sc.Providers)
		}
		if p.Scheme != "op" {
			t.Fatalf("scheme = %q, want op", p.Scheme)
		}
		if strings.Join(p.Command, " ") != "op read {ref}" {
			t.Fatalf("command = %v", p.Command)
		}
	})

	t.Run("legacy block carries its command and env_override", func(t *testing.T) {
		cfg := Config{Secret: SecretResolver{
			Command:     []string{"op", "read", "--no-newline", "{ref}"},
			EnvOverride: true,
		}}
		sc := NormalizeSecrets(cfg)
		if sc.EnvOverride == nil || !*sc.EnvOverride {
			t.Fatalf("env_override should normalize from legacy block")
		}
		p := sc.Providers["onepassword"]
		if strings.Join(p.Command, " ") != "op read --no-newline {ref}" {
			t.Fatalf("command = %v", p.Command)
		}
	})
}

// TestNormalizeSecretsProviders covers the new secrets.providers path, including
// scheme defaulting from the map key and env_override precedence.
func TestNormalizeSecretsProviders(t *testing.T) {
	cfg := Config{
		Secret: SecretResolver{EnvOverride: true}, // should be overridden by Secrets.EnvOverride
		Secrets: SecretsConfig{
			EnvOverride: boolPtr(false),
			Providers: map[string]ProviderConfig{
				"onepassword": {}, // scheme + command default
			},
		},
	}
	sc := NormalizeSecrets(cfg)
	if sc.EnvOverride == nil || *sc.EnvOverride {
		t.Fatalf("Secrets.EnvOverride (false) should win over legacy (true)")
	}
	p := sc.Providers["onepassword"]
	if p.Scheme != "op" {
		t.Fatalf("scheme should default from alias to op, got %q", p.Scheme)
	}
	if strings.Join(p.Command, " ") != "op read {ref}" {
		t.Fatalf("command should default, got %v", p.Command)
	}
}

func TestNormalizeSecretsIdempotent(t *testing.T) {
	cfg := Config{Secret: SecretResolver{Command: []string{"op", "read", "{ref}"}}}
	once := NormalizeSecrets(cfg)
	twice := NormalizeSecrets(Config{Secrets: once})
	if len(twice.Providers) != 1 {
		t.Fatalf("re-normalizing changed provider count: %v", twice.Providers)
	}
	p := twice.Providers["onepassword"]
	if p.Scheme != "op" || strings.Join(p.Command, " ") != "op read {ref}" {
		t.Fatalf("re-normalize not idempotent: %+v", p)
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name      string
		providers map[string]ProviderConfig
		wantErr   string // "" = valid
	}{
		// Every kind the registry can build is accepted, by inference and by type:.
		{"op inferred from key", map[string]ProviderConfig{"onepassword": {}}, ""},
		{"op explicit", map[string]ProviderConfig{"onepassword": {Type: "onepassword", Scheme: "op"}}, ""},
		{"exec inferred from key", map[string]ProviderConfig{"pass": {Command: []string{"pass", "show", "{ref}"}}}, ""},
		{"exec custom scheme", map[string]ProviderConfig{"secrets": {Type: "exec", Scheme: "vault", Command: []string{"vault", "read", "{ref}"}}}, ""},
		{"file inferred from key", map[string]ProviderConfig{"file": {}}, ""},
		{"file custom scheme", map[string]ProviderConfig{"mounted": {Type: "file", Scheme: "k8s"}}, ""},
		{"one of each kind", map[string]ProviderConfig{
			"onepassword": {},
			"pass":        {Type: "exec", Command: []string{"pass", "show", "{ref}"}},
			"file":        {Type: "file"},
		}, ""},

		{"unknown type rejected", map[string]ProviderConfig{"v": {Type: "vault", Command: []string{"x"}}}, `unknown type "vault" (want onepassword|exec|file)`},
		{"invalid scheme rejected", map[string]ProviderConfig{"p": {Type: "exec", Scheme: "my_scheme", Command: []string{"x"}}}, `invalid scheme "my_scheme"`},
		{"exec without command rejected", map[string]ProviderConfig{"vault": {Scheme: "vault"}}, "type exec needs a command"},
		{"onepassword off scheme op rejected", map[string]ProviderConfig{"p": {Type: "onepassword", Scheme: "vault"}}, `serves only scheme op, not "vault"`},
		{"duplicate scheme rejected", map[string]ProviderConfig{
			"a": {Type: "file", Scheme: "f"},
			"b": {Type: "file", Scheme: "f"},
		}, `secrets providers "a" and "b" both serve scheme "f"`},

		// The service_account_token rule is op-only and still exactly-one-of.
		{"op single-source token accepted", map[string]ProviderConfig{"onepassword": {Scheme: "op", Auth: ProviderAuth{
			ServiceAccountToken: &SecretSource{File: "/x"},
		}}}, ""},
		{"op two-source token rejected", map[string]ProviderConfig{"onepassword": {Scheme: "op", Auth: ProviderAuth{
			ServiceAccountToken: &SecretSource{File: "/x", Env: "Y"},
		}}}, "exactly one of file|value|env"},
		{"token on exec rejected", map[string]ProviderConfig{"pass": {Command: []string{"pass"}, Auth: ProviderAuth{
			ServiceAccountToken: &SecretSource{Env: "Y"},
		}}}, "service_account_token applies only to type onepassword, not exec"},
		{"token on file rejected", map[string]ProviderConfig{"file": {Auth: ProviderAuth{
			ServiceAccountToken: &SecretSource{Env: "Y"},
		}}}, "service_account_token applies only to type onepassword, not file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := Config{Secrets: SecretsConfig{Providers: tc.providers}}
			// Raw and normalized configs must validate identically.
			for _, c := range []*Config{&raw, {Secrets: NormalizeSecrets(raw)}} {
				err := ValidateConfig(c)
				if tc.wantErr == "" {
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					continue
				}
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
				}
				var ce *ConfigError
				if !errors.As(err, &ce) {
					t.Fatalf("got %T, want *ConfigError (exit 2)", err)
				}
			}
		})
	}
}

// TestValidateConfigAcceptsEveryProviderKind ties validation to the kind list
// the secret registry dispatches on: a kind added to ProviderKinds is accepted
// with no further edit to ValidateConfig.
func TestValidateConfigAcceptsEveryProviderKind(t *testing.T) {
	for _, kind := range ProviderKinds {
		scheme := "x" + kind
		if kind == ProviderKindOnePassword {
			scheme = "op"
		}
		c := &Config{Secrets: SecretsConfig{Providers: map[string]ProviderConfig{
			kind: {Type: kind, Scheme: scheme, Command: []string{"tool", "{ref}"}},
		}}}
		if err := ValidateConfig(c); err != nil {
			t.Errorf("kind %q rejected: %v", kind, err)
		}
	}
}

// TestREADMESecretsExampleLoads loads the README's Secrets config block verbatim
// as config.yaml — the documented exec/file providers must survive Load.
func TestREADMESecretsExampleLoads(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	start := strings.Index(readme, "```yaml\nsecrets:")
	if start < 0 {
		t.Fatal("README has no ```yaml block starting with secrets:")
	}
	block := readme[start+len("```yaml\n"):]
	block = block[:strings.Index(block, "```")]

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(block), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(README secrets block): %v", err)
	}
	for name, kind := range map[string]string{
		"onepassword": ProviderKindOnePassword,
		"pass":        ProviderKindExec,
		"file":        ProviderKindFile,
	} {
		p, ok := loaded.Config.Secrets.Providers[name]
		if !ok {
			t.Fatalf("README example lost provider %q", name)
		}
		if got := ProviderKind(p.Type, p.Scheme); got != kind {
			t.Errorf("provider %q kind = %q, want %q", name, got, kind)
		}
	}
}
