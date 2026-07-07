package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/jedwards1230/anyctl/internal/command"
	"github.com/jedwards1230/anyctl/internal/manifest"
)

// TestExecuteIncompleteServiceErrors proves the completeness gate at the top of
// Execute rejects a portable-but-unbound service (no base_url) with a
// *manifest.ConfigError — even on dry-run — so the CLI/MCP classify it to exit 2.
func TestExecuteIncompleteServiceErrors(t *testing.T) {
	// A portable manifest: structurally valid, but no base_url and an unbound
	// secret. It must never execute (nor dry-run) until a profile binds it.
	svc := &manifest.Service{
		Name:    "radarr",
		Auth:    manifest.Auth{Strategy: "header-key", Header: "X-Api-Key", Value: "{secret.api_key}"},
		Secrets: map[string]manifest.Secret{"api_key": {}},
		Commands: map[string]manifest.Command{
			"list": {Method: "GET", Path: "/api/v3/movie"},
		},
	}
	cmds := command.FromManifest(svc)

	for _, dryRun := range []bool{false, true} {
		_, err := Execute(context.Background(), Request{
			Config:  manifest.Config{},
			Service: svc,
			Command: cmds["list"],
			Runner:  fakeOp,
			Flags:   Flags{DryRun: dryRun},
			Getenv:  func(string) string { return "" },
		}, nil)
		if err == nil {
			t.Fatalf("dryRun=%v: incomplete service should fail the completeness gate", dryRun)
		}
		var cfgErr *manifest.ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("dryRun=%v: want *manifest.ConfigError, got %T: %v", dryRun, err, err)
		}
	}
}

// TestExecuteCompleteServiceStillRuns confirms a complete service passes the gate
// and dry-runs as before (the gate is transparent to valid configs).
func TestExecuteCompleteServiceStillRuns(t *testing.T) {
	svc := newService("https://movies.example.com")
	cmds := command.FromManifest(svc)
	res, err := Execute(context.Background(), Request{
		Config:  manifest.Config{},
		Service: svc,
		Command: cmds["list"],
		Runner:  fakeOp,
		Flags:   Flags{DryRun: true},
		Getenv:  func(string) string { return "" },
	}, nil)
	if err != nil {
		t.Fatalf("complete service should pass the gate: %v", err)
	}
	if res.DryRunMsg == "" {
		t.Fatal("expected a dry-run preview")
	}
}
