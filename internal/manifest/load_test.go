package manifest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadStrictConfigRejectsUnknownKey proves a typo'd top-level config key is
// rejected (strict decoding) and classifies as a *ConfigError (exit 2).
func TestLoadStrictConfigRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "version: 1\nop_vault: vault\n")
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected an error for an unknown top-level config key")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("unknown config key should be a *ConfigError, got %T: %v", err, err)
	}
}

// TestLoadConfigSecretAndSecretsCoexist proves a config carrying BOTH the legacy
// secret: block and the new secrets: block still loads clean under strict decode.
func TestLoadConfigSecretAndSecretsCoexist(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `version: 1
secret:
  resolver: op
  command: ["op", "read", "{ref}"]
  env_override: true
secrets:
  env_override: true
  providers:
    onepassword:
      scheme: op
      command: ["op", "read", "{ref}"]
`)
	if _, err := Load(dir); err != nil {
		t.Fatalf("secret: and secrets: should coexist: %v", err)
	}
}

func writeManifest(t *testing.T, dir, name, body string) {
	t.Helper()
	svcDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMergeDefaults(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "tdarr.yaml", `
auth: { strategy: none }
`)
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := l.Services["tdarr"]
	if !ok {
		t.Fatal("tdarr not loaded (name should default to filename stem)")
	}
	if svc.Transport != "http" {
		t.Errorf("transport default = %q, want http", svc.Transport)
	}
	if svc.Timeout != "60s" {
		t.Errorf("timeout default = %q, want 60s", svc.Timeout)
	}
	if svc.TimeoutDuration().Seconds() != 60 {
		t.Errorf("timeout = %v, want 60s", svc.TimeoutDuration())
	}
}

func TestLoadMissingDir(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	// anyctl ships no built-in floor: a missing/empty config dir yields ZERO
	// services (no installed catalogs, no local services/).
	if len(l.Services) != 0 {
		t.Errorf("missing dir loaded %d services, want 0 (no embedded floor)", len(l.Services))
	}
}

func TestValidateRejectsBad(t *testing.T) {
	// Fixtures are portable (no base_url/ref) so each case exercises its INTENDED
	// rule — an in-manifest base_url would otherwise short-circuit on the
	// "no in-manifest binding" rule below. The binding-rule cases are explicit.
	cases := map[string]*Service{
		"bad transport":        {Name: "x", Transport: "carrier-pigeon"},
		"bad strategy":         {Name: "x", Auth: Auth{Strategy: "telepathy"}},
		"undeclared secret":    {Name: "x", Auth: Auth{Strategy: "header-key", Header: "X", Value: "{secret.missing}"}},
		"header-key no header": {Name: "x", Auth: Auth{Strategy: "header-key", Value: "v"}},
		"bad svc pagination style": {
			Name:       "x",
			Pagination: Pagination{Style: "infinite-scroll"},
		},
		"bad cmd pagination style": {
			Name: "x",
			Commands: map[string]Command{
				"list": {Method: "GET", Path: "/", Pagination: Pagination{Style: "magic"}},
			},
		},
		// The "no in-manifest binding" rule: base_url / endpoint base_url / secret
		// ref all belong in profile.yaml, never the manifest.
		"in-manifest base_url": {Name: "x", BaseURL: "https://h.example.com"},
		"in-manifest endpoint base_url": {
			Name:      "x",
			Endpoints: map[string]Endpoint{"alt": {BaseURL: "https://alt.example.com"}},
		},
		"in-manifest secret ref": {
			Name:    "x",
			Secrets: map[string]Secret{"api_key": {Ref: "op://vault/X/api_key"}},
		},
	}
	for name, svc := range cases {
		if err := Validate(svc); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

// TestValidateAcceptsGood proves a well-formed PORTABLE manifest passes
// structural Validate: no in-manifest base_url and an env-only secret slot (env
// is a permitted in-manifest override; ref/base_url are not). Completeness
// (a bound base_url + secrets) is ValidateComplete's job, not Validate's.
func TestValidateAcceptsGood(t *testing.T) {
	svc := &Service{
		Name:    "radarr",
		Auth:    Auth{Strategy: "header-key", Header: "X-Api-Key", Value: "{secret.api_key}"},
		Secrets: map[string]Secret{"api_key": {Env: "RADARR_API_KEY"}},
		Commands: map[string]Command{
			"status": {Method: "GET", Path: "/api/v3/system/status"},
		},
	}
	if err := Validate(svc); err != nil {
		t.Fatalf("valid portable manifest rejected: %v", err)
	}
}

func TestSecretRefsExtraction(t *testing.T) {
	got := secretRefs(`{secret.username}:{secret.password}`)
	if len(got) != 2 || got[0] != "username" || got[1] != "password" {
		t.Fatalf("got %v", got)
	}
}

// TestLoadWarnsOnUnknownKeys: the runtime load (svc/list/doctor/mcp) must not
// hard-fail on a manifest carrying an unknown key — that would brick every
// command after an upgrade. It loads, records the keys, and warns once; only
// LoadService (lint <file>) rejects them, as a *ConfigError.
func TestLoadWarnsOnUnknownKeys(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantKeys []string
	}{
		{"clean", "name: svc\ncommands:\n  list:\n    method: GET\n    path: /x\n", nil},
		{"annotations", "name: svc\nannotations: {x: {y: [1]}}\ncommands:\n  list:\n    method: GET\n    path: /x\n    annotations: {z: 1}\n", nil},
		{"top-level", "name: svc\ndescripton: typo\n", []string{`line 2: "descripton"`}},
		{
			"command and step",
			"name: svc\ncommands:\n  list:\n    method: GET\n    ouput: {mode: json}\n    steps:\n      - path: /a\n        extrct: {x: .x}\n",
			[]string{`line 5: "ouput"`, `line 8: "extrct"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, "svc.yaml", tc.body)
			var buf bytes.Buffer
			l, err := load(dir, &buf)
			if err != nil {
				t.Fatalf("Load must not fail on unknown keys: %v", err)
			}
			svc := l.Services["svc"]
			_, lintErr := LoadService(filepath.Join(dir, "services", "svc.yaml"), Config{})

			if tc.wantKeys == nil {
				if svc.UnknownKeys() != nil || buf.Len() != 0 || lintErr != nil {
					t.Fatalf("clean manifest flagged: keys=%v warn=%q lint=%v", svc.UnknownKeys(), buf.String(), lintErr)
				}
				return
			}
			var uk *UnknownKeysError
			if !errors.As(svc.UnknownKeys(), &uk) || !slices.Equal(uk.Keys, tc.wantKeys) {
				t.Fatalf("UnknownKeys = %v, want keys %v", svc.UnknownKeys(), tc.wantKeys)
			}
			if got := buf.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "warning") {
				t.Fatalf("warning = %q, want exactly one warning line", got)
			}
			var ce *ConfigError
			if !errors.As(lintErr, &ce) || !errors.As(lintErr, &uk) {
				t.Fatalf("LoadService err = %v, want *ConfigError wrapping *UnknownKeysError", lintErr)
			}
		})
	}
}

// TestDecodeManifestKeepsTypeErrors: a non-key decode failure (a type mismatch)
// is still a hard parse error, not reclassified as an unknown key.
func TestDecodeManifestKeepsTypeErrors(t *testing.T) {
	_, err := DecodeManifest([]byte("name: svc\ncommands: [not, a, map]\nbogus: 1\n"))
	var ce *ConfigError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("err = %v, want a parse *ConfigError", err)
	}
	if svc, err := DecodeManifest(nil); err != nil || svc == nil {
		t.Fatalf("empty manifest: svc=%v err=%v, want zero Service", svc, err)
	}
}

// TestLoadWarnsOnUnknownKeysInstalledCatalog: an installed-catalog manifest takes
// the same lenient path as a local one — Load succeeds and warns exactly once.
func TestLoadWarnsOnUnknownKeysInstalledCatalog(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(CatalogsDir(dir), "acme")
	if err := os.MkdirAll(catDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: svc\ndescripton: typo\ncommands:\n  list:\n    method: GET\n    path: /x\n"
	if err := os.WriteFile(filepath.Join(catDir, "svc.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	l, err := load(dir, &buf)
	if err != nil {
		t.Fatalf("Load must not fail on unknown keys: %v", err)
	}
	if l.Services["acme:svc"].UnknownKeys() == nil {
		t.Fatal("UnknownKeys = nil, want the typo recorded")
	}
	if got := buf.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, `catalogs/acme/svc.yaml: unknown key(s): line 2: "descripton"`) {
		t.Fatalf("warning = %q, want exactly one line naming the catalog file and key", got)
	}
}

// TestUnknownKeyNamedWhenValidateFails: a typo that also breaks a structural
// rule (`metod:` leaves method unset) must still name the unknown key, and stay
// classifiable as both *ConfigError and *UnknownKeysError.
func TestUnknownKeyNamedWhenValidateFails(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "svc.yaml", "name: svc\ncommands:\n  list:\n    metod: GET\n    path: /x\n")
	_, err := LoadService(filepath.Join(dir, "services", "svc.yaml"), Config{})
	var ce *ConfigError
	var uk *UnknownKeysError
	if !errors.As(err, &ce) || !errors.As(err, &uk) || !strings.Contains(err.Error(), `"metod"`) {
		t.Fatalf("err = %v, want a *ConfigError that also names \"metod\"", err)
	}
}
