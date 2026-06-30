package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// apiKeyHeaderSpec is a small OpenAPI 3.0 document with an apiKey-in-header
// security scheme, used by several tests below.
const apiKeyHeaderSpec = `openapi: "3.0.3"
info:
  title: Demo Service
  description: |
    A small demo API.
    Second line should not appear.
  version: "1.0"
paths:
  /widgets:
    get:
      operationId: listWidgets
      summary: List widgets
      responses:
        "200": { description: ok }
  /widgets/{id}:
    get:
      operationId: getWidget
      summary: Get a widget by id
      responses:
        "200": { description: ok }
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-Api-Key
security:
  - ApiKeyAuth: []
`

// TestGenerateManifestFromSpecAPIKeyHeader covers the primary materialize path:
// portable output, expected commands, header-key auth, schema+structural
// validation, and a full load through the install path.
func TestGenerateManifestFromSpecAPIKeyHeader(t *testing.T) {
	out, err := GenerateManifestFromSpec("demo", []byte(apiKeyHeaderSpec))
	if err != nil {
		t.Fatalf("GenerateManifestFromSpec: %v", err)
	}

	for _, forbidden := range []string{"base_url:", "spec:", "ref:"} {
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.HasPrefix(trimmed, forbidden) {
				t.Errorf("generated manifest has an active %q line: %q", forbidden, line)
			}
		}
	}

	if name, err := ValidatePortableManifest(out); err != nil {
		t.Fatalf("ValidatePortableManifest: %v\n---\n%s", err, out)
	} else if name != "demo" {
		t.Errorf("ValidatePortableManifest name = %q, want demo", name)
	}

	if !strings.Contains(string(out), "strategy: header-key") {
		t.Errorf("expected header-key auth strategy:\n%s", out)
	}
	if !strings.Contains(string(out), "header: X-Api-Key") {
		t.Errorf("expected header: X-Api-Key:\n%s", out)
	}
	if !strings.Contains(string(out), "value: '{secret.api_key}'") && !strings.Contains(string(out), `value: "{secret.api_key}"`) {
		t.Errorf("expected a quoted {secret.api_key} value template:\n%s", out)
	}
	if !strings.Contains(string(out), "listwidgets:") || !strings.Contains(string(out), "getwidget:") {
		t.Errorf("expected listwidgets/getwidget commands:\n%s", out)
	}
	if !strings.Contains(string(out), "path: /widgets") {
		t.Errorf("expected /widgets path:\n%s", out)
	}
	if !strings.Contains(string(out), "Demo Service") {
		t.Errorf("expected the info.title in the description:\n%s", out)
	}

	// Full round-trip: write it where `catalog edit`-style local services live and
	// load it through the real loader.
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.yaml")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := LoadService(path, Config{})
	if err != nil {
		t.Fatalf("LoadService: %v\n---\n%s", err, out)
	}
	if svc.Name != "demo" {
		t.Errorf("loaded service name = %q, want demo", svc.Name)
	}
	if _, ok := svc.Commands["listwidgets"]; !ok {
		t.Errorf("loaded service missing listwidgets command: %+v", svc.Commands)
	}
	if svc.Auth.Strategy != "header-key" || svc.Auth.Header != "X-Api-Key" {
		t.Errorf("loaded auth = %+v, want header-key/X-Api-Key", svc.Auth)
	}
}

// TestGenerateManifestFromSpecCommandUniqueness asserts that two operations
// which would collide on the same inferred command key are rejected with a
// clear error rather than silently dropping one.
func TestGenerateManifestFromSpecCommandUniqueness(t *testing.T) {
	const dup = `openapi: "3.0.3"
info: { title: Dup, version: "1.0" }
paths:
  /a:
    get:
      operationId: doThing
      responses: { "200": { description: ok } }
  /b:
    get:
      operationId: doThing
      responses: { "200": { description: ok } }
`
	_, err := GenerateManifestFromSpec("dup", []byte(dup))
	if err == nil {
		t.Fatal("expected a command-key collision error, got nil")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("collision should be a *ConfigError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "doThing") {
		t.Errorf("error should name the colliding operations: %v", err)
	}
}

// TestGenerateManifestFromSpecAuthMappings covers each auth-scheme mapping,
// including the graceful "none + comment" fallbacks.
func TestGenerateManifestFromSpecAuthMappings(t *testing.T) {
	const opTail = `
paths:
  /ping:
    get:
      operationId: ping
      responses: { "200": { description: ok } }
`
	cases := []struct {
		name           string
		schemesYAML    string
		wantStrategy   string
		wantContains   []string // additional substrings expected in the output
		wantNoneReason string   // substring expected in the explanatory comment when strategy is none
	}{
		{
			name: "bearer",
			schemesYAML: `
components:
  securitySchemes:
    BearerAuth: { type: http, scheme: bearer }
security:
  - BearerAuth: []
`,
			wantStrategy: "bearer",
			wantContains: []string{"scheme: Bearer", "{secret.token}"},
		},
		{
			name: "basic",
			schemesYAML: `
components:
  securitySchemes:
    BasicAuth: { type: http, scheme: basic }
security:
  - BasicAuth: []
`,
			wantStrategy: "basic",
			wantContains: []string{"{secret.username}", "{secret.password}"},
		},
		{
			name: "oauth2-client-credentials-absolute-url",
			schemesYAML: `
components:
  securitySchemes:
    OAuth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://auth.example.com/token
          scopes: {}
security:
  - OAuth: []
`,
			wantStrategy: "oauth2-client-credentials",
			wantContains: []string{"token_url: https://auth.example.com/token", "{secret.client_id}", "{secret.client_secret}"},
		},
		{
			name: "oauth2-client-credentials-relative-url-placeholder",
			schemesYAML: `
components:
  securitySchemes:
    OAuth:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: /oauth/token
          scopes: {}
security:
  - OAuth: []
`,
			wantStrategy:   "oauth2-client-credentials",
			wantContains:   []string{"CHANGE-ME"},
			wantNoneReason: "",
		},
		{
			name: "apikey-query-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    QueryKey: { type: apiKey, in: query, name: api_key }
security:
  - QueryKey: []
`,
			wantStrategy:   "none",
			wantNoneReason: "in: query",
		},
		{
			name: "apikey-cookie-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    CookieKey: { type: apiKey, in: cookie, name: session }
security:
  - CookieKey: []
`,
			wantStrategy:   "none",
			wantNoneReason: "in: cookie",
		},
		{
			name: "oauth2-without-clientcredentials-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    OAuth:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://auth.example.com/authorize
          tokenUrl: https://auth.example.com/token
          scopes: {}
security:
  - OAuth: []
`,
			wantStrategy:   "none",
			wantNoneReason: "clientCredentials",
		},
		{
			name: "openidconnect-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    OIDC:
      type: openIdConnect
      openIdConnectUrl: https://auth.example.com/.well-known/openid-configuration
security:
  - OIDC: []
`,
			wantStrategy:   "none",
			wantNoneReason: "openIdConnect",
		},
		{
			name:           "no-security-schemes-graceful-none",
			schemesYAML:    "",
			wantStrategy:   "none",
			wantNoneReason: "no components.securitySchemes",
		},
		{
			name: "mutualtls-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    MTLS: { type: mutualTLS }
security:
  - MTLS: []
`,
			wantStrategy:   "none",
			wantNoneReason: "mutualTLS",
		},
		{
			name: "http-digest-graceful-none",
			schemesYAML: `
components:
  securitySchemes:
    DigestAuth: { type: http, scheme: digest }
security:
  - DigestAuth: []
`,
			wantStrategy:   "none",
			wantNoneReason: "digest",
		},
		{
			name: "no-global-security-falls-back-to-first-declared-scheme",
			schemesYAML: `
components:
  securitySchemes:
    BearerAuth: { type: http, scheme: bearer }
`,
			wantStrategy: "bearer",
			wantContains: []string{"{secret.token}"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := "openapi: \"3.0.3\"\ninfo: { title: Auth Test, version: \"1.0\" }\n" + tc.schemesYAML + opTail
			out, err := GenerateManifestFromSpec("authtest", []byte(doc))
			if err != nil {
				t.Fatalf("GenerateManifestFromSpec: %v\n---\n%s", err, doc)
			}
			if _, err := ValidatePortableManifest(out); err != nil {
				t.Fatalf("ValidatePortableManifest: %v\n---\n%s", err, out)
			}
			if !strings.Contains(string(out), "strategy: "+tc.wantStrategy) {
				t.Errorf("expected strategy: %s in:\n%s", tc.wantStrategy, out)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("expected %q in:\n%s", want, out)
				}
			}
			if tc.wantNoneReason != "" {
				if !strings.Contains(string(out), tc.wantNoneReason) {
					t.Errorf("expected explanatory comment containing %q in:\n%s", tc.wantNoneReason, out)
				}
				// The comment must actually be a YAML comment, not active content.
				found := false
				for _, line := range strings.Split(string(out), "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, tc.wantNoneReason) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("explanatory text %q should appear in a # comment line:\n%s", tc.wantNoneReason, out)
				}
			}
		})
	}
}

// TestGenerateManifestFromSpec31 is a smoke test against an OpenAPI 3.1
// document (libopenapi supports both 3.0 and 3.1).
func TestGenerateManifestFromSpec31(t *testing.T) {
	const doc31 = `openapi: "3.1.0"
info:
  title: ThreeOne
  version: "1.0"
paths:
  /health:
    get:
      operationId: health
      responses:
        "200": { description: ok }
components:
  securitySchemes:
    BearerAuth: { type: http, scheme: bearer }
security:
  - BearerAuth: []
`
	out, err := GenerateManifestFromSpec("threeone", []byte(doc31))
	if err != nil {
		t.Fatalf("GenerateManifestFromSpec(3.1 doc): %v", err)
	}
	if _, err := ValidatePortableManifest(out); err != nil {
		t.Fatalf("ValidatePortableManifest: %v\n---\n%s", err, out)
	}
	if !strings.Contains(string(out), "strategy: bearer") {
		t.Errorf("expected bearer strategy:\n%s", out)
	}
	if !strings.Contains(string(out), "health:") {
		t.Errorf("expected health command:\n%s", out)
	}
}

// TestGenerateManifestFromSpecSwagger2Rejected confirms a Swagger 2.0 document
// surfaces as a clean *ConfigError, the same classification as parseOperations.
func TestGenerateManifestFromSpecSwagger2Rejected(t *testing.T) {
	const swagger2 = `swagger: "2.0"
info:
  title: Old API
  version: "1.0"
paths: {}
`
	_, err := GenerateManifestFromSpec("old", []byte(swagger2))
	if err == nil {
		t.Fatal("expected an error for a Swagger 2.0 document, got nil")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("swagger 2.0 should be a *ConfigError, got %T: %v", err, err)
	}
}

// TestInferServiceName covers title-based slugging and the no-title case.
func TestInferServiceName(t *testing.T) {
	name, err := InferServiceName([]byte(apiKeyHeaderSpec))
	if err != nil {
		t.Fatal(err)
	}
	if name != "demo-service" {
		t.Errorf("InferServiceName = %q, want demo-service", name)
	}

	const noTitle = `openapi: "3.0.3"
info: { version: "1.0" }
paths: {}
`
	name, err = InferServiceName([]byte(noTitle))
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		t.Errorf("InferServiceName(no title) = %q, want empty", name)
	}

	// Swagger 2.0 propagates the same error InferredCommands/parseOperations give.
	const swagger2 = `swagger: "2.0"
info: { title: Old, version: "1.0" }
paths: {}
`
	if _, err := InferServiceName([]byte(swagger2)); err == nil {
		t.Fatal("expected an error for a Swagger 2.0 document")
	}
}
