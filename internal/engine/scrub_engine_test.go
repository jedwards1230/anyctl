package engine

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/jedwards1230/labctl/internal/command"
	"github.com/jedwards1230/labctl/internal/manifest"
)

const queryAuthSecret = "QUERY-APIKEY-VALUE"

// queryAuthService points its auth at a query param: ?apikey={secret.X}. This is
// the field-position the value-scrubber exists for — a header redactor can't see
// a secret that lands in the URL.
func queryAuthService(baseURL string) *manifest.Service {
	return &manifest.Service{
		Name:    "qsvc",
		BaseURL: baseURL,
		Auth:    manifest.Auth{Strategy: "none"},
		Secrets: map[string]manifest.Secret{"api_key": {Ref: "op://v/i/api_key"}},
		Commands: map[string]manifest.Command{
			"list": {Method: "GET", Path: "/api/movie", Query: "apikey={secret.api_key}"},
		},
	}
}

// TestScrubVerboseQueryParam asserts a {secret.X} resolved into the URL query is
// NOT echoed verbatim in -v output, and appears as <redacted> instead.
func TestScrubVerboseQueryParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != queryAuthSecret {
			t.Errorf("server did not receive the resolved apikey: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	svc := queryAuthService(srv.URL)
	cmds := command.FromManifest(svc)
	var verbose bytes.Buffer
	_, err := Execute(context.Background(), Request{
		Config:  manifest.Config{},
		Service: svc,
		Command: cmds["list"],
		Runner:  func([]string) (string, error) { return queryAuthSecret, nil },
		Flags:   Flags{Verbose: true},
		Getenv:  func(string) string { return "" },
	}, &verbose)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := verbose.String()
	if strings.Contains(out, queryAuthSecret) {
		t.Fatalf("verbose output leaked the query-param secret:\n%s", out)
	}
	if !strings.Contains(out, "apikey=<redacted>") {
		t.Fatalf("verbose output should show apikey=<redacted>:\n%s", out)
	}
}

// TestScrubErrorOn400 asserts a ≥400 error string does not carry the resolved
// query-param secret in its URL.
func TestScrubErrorOn400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	svc := queryAuthService(srv.URL)
	cmds := command.FromManifest(svc)
	_, err := Execute(context.Background(), Request{
		Config:  manifest.Config{},
		Service: svc,
		Command: cmds["list"],
		Runner:  func([]string) (string, error) { return queryAuthSecret, nil },
		Getenv:  func(string) string { return "" },
	}, nil)
	if err == nil {
		t.Fatal("expected an HTTP error")
	}
	if strings.Contains(err.Error(), queryAuthSecret) {
		t.Fatalf("error string leaked the secret: %v", err)
	}
}

// TestScrubWSParams asserts a {secret.X} resolved into jsonrpc-ws command params
// is redacted in the id=2 verbose send line.
func TestScrubWSParams(t *testing.T) {
	url := engineWSServer(t, func(conn *websocket.Conn) {
		defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck
		_ = wsRead(t, conn)                                 // id=1 auth
		wsWrite(t, conn, map[string]any{"id": 1, "result": true})
		_ = wsRead(t, conn) // id=2 call
		wsWrite(t, conn, map[string]any{"id": 2, "result": "ok"})
	})

	svc := &manifest.Service{
		Name:      "truenas",
		BaseURL:   url,
		Transport: "jsonrpc-ws",
		Timeout:   "5s",
		Auth: manifest.Auth{
			Strategy: "ws-login",
			Method:   "auth.login_with_api_key",
			Params:   []string{"{secret.api_key}"},
		},
		Secrets: map[string]manifest.Secret{"api_key": {Ref: "op://v/i/api_key"}},
		Commands: map[string]manifest.Command{
			// The secret also flows through the command params here.
			"call": {Method: "system.info", Params: `["{secret.api_key}"]`},
		},
	}
	cmds := command.FromManifest(svc)
	var verbose bytes.Buffer
	_, err := Execute(context.Background(), Request{
		Config:  manifest.Config{},
		Service: svc,
		Command: cmds["call"],
		Runner:  func([]string) (string, error) { return queryAuthSecret, nil },
		Flags:   Flags{Verbose: true},
		Getenv:  func(string) string { return "" },
	}, &verbose)
	if err != nil {
		t.Fatalf("execute ws: %v", err)
	}
	out := verbose.String()
	if strings.Contains(out, queryAuthSecret) {
		t.Fatalf("ws verbose leaked the params secret:\n%s", out)
	}
	if !strings.Contains(out, "id=2") || !strings.Contains(out, "<redacted>") {
		t.Fatalf("ws id=2 params should be redacted:\n%s", out)
	}
}
