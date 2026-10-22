package mcpserver_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/jedwards1230/anyctl/internal/mcpserver"
)

// The protocol revisions are asserted verbatim rather than read back from the
// SDK, so an SDK-side rename or default change cannot make these tests pass
// vacuously.
const (
	// legacyProtocol is what ContextForge negotiates with anyctl through
	// `initialize` today.
	legacyProtocol = "2025-11-25"
	// modernProtocol is the sessionless revision (SEP-2575). go-sdk serves it
	// over streamable HTTP only when the handler is stateless.
	modernProtocol = "2026-07-28"
)

// mcpCall is one JSON-RPC message POSTed to /mcp.
type mcpCall struct {
	method string
	params map[string]any
	// notification omits the JSON-RPC id (the SDK answers 202, no body).
	notification bool
	// header carries the extra request headers (MCP-Protocol-Version,
	// Mcp-Session-Id, Mcp-Method, ...).
	header http.Header
}

// mcpReply is the decoded response to an mcpCall.
type mcpReply struct {
	status int
	header http.Header
	raw    string
	env    map[string]any // the JSON-RPC envelope; nil when the body is not one
}

// newProtocolTestServer stands up the production NewHTTPHandler mux in front of
// an upstream answering {"status":"ok"}, so the /mcp handler under test is
// exactly the one ServeHTTP serves.
func newProtocolTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	loaded := buildTestLoaded(upstream.URL)
	tracer := noop.NewTracerProvider().Tracer("test")
	ts := httptest.NewServer(mcpserver.NewHTTPHandler(loaded, loaded.Config, "v0", tracer, nil, mcpserver.Options{}, ""))
	t.Cleanup(ts.Close)
	return ts
}

// postMCP sends call to ts's /mcp endpoint and decodes the reply.
func postMCP(t *testing.T, ts *httptest.Server, call mcpCall) mcpReply {
	t.Helper()

	msg := map[string]any{"jsonrpc": "2.0", "method": call.method}
	if !call.notification {
		msg["id"] = 1
	}
	if call.params != nil {
		msg["params"] = call.params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal %s: %v", call.method, err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, vs := range call.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", call.method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s body: %v", call.method, err)
	}
	return mcpReply{
		status: resp.StatusCode,
		header: resp.Header,
		raw:    string(raw),
		env:    decodeRPCEnvelope(resp.Header.Get("Content-Type"), raw),
	}
}

// decodeRPCEnvelope decodes a streamable-HTTP response body. The handler
// answers calls with an SSE stream by default, so the envelope arrives as the
// payload of a `data:` line; an application/json body (e.g. a protocol
// rejection) is decoded directly.
func decodeRPCEnvelope(contentType string, raw []byte) map[string]any {
	decode := func(b []byte) map[string]any {
		var env map[string]any
		if err := json.Unmarshal(b, &env); err != nil {
			return nil
		}
		if _, ok := env["jsonrpc"]; !ok {
			return nil
		}
		return env
	}
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return decode(raw)
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
			if env := decode([]byte(strings.TrimSpace(data))); env != nil {
				return env
			}
		}
	}
	return nil
}

// resultOf returns the reply's JSON-RPC result object, failing the test on a
// non-200 status or a JSON-RPC error (the -32022 "protocol version ... is not
// supported by this server" rejection lands here).
func (r mcpReply) resultOf(t *testing.T, what string) map[string]any {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200 (body: %s)", what, r.status, r.raw)
	}
	if r.env == nil {
		t.Fatalf("%s: no JSON-RPC envelope in body: %s", what, r.raw)
	}
	if rpcErr, ok := r.env["error"]; ok {
		t.Fatalf("%s: JSON-RPC error %v (body: %s)", what, rpcErr, r.raw)
	}
	result, ok := r.env["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: want an object result, got %#v (body: %s)", what, r.env["result"], r.raw)
	}
	return result
}

// assertPingResult checks a tools/call testsvc_ping result actually came from
// the tool handler (not an error result): the upstream answers {"status":"ok"}.
func assertPingResult(t *testing.T, result map[string]any) {
	t.Helper()
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("testsvc_ping returned a tool error: %#v", result)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("testsvc_ping: no content in result %#v", result)
	}
	first, _ := content[0].(map[string]any)
	if text, _ := first["text"].(string); !strings.Contains(text, "ok") {
		t.Fatalf("testsvc_ping: content[0].text = %q, want it to contain \"ok\"", text)
	}
}

// TestHTTPLegacyCallWithProtocolMetaIsAccepted replays ContextForge's exact
// federation pattern (v1.1.0+): negotiate 2025-11-25 with `initialize`, then
// forward the downstream client's `_meta` — including the 2026-07-28-era key
// io.modelcontextprotocol/protocolVersion — on tools/call.
//
// A stateful handler (StreamableHTTPOptions nil) rejects ANY call carrying
// that key with HTTP 400 / -32022 `protocol version "2025-11-25" is not
// supported by this server`, which broke every federated tool call in
// jedwards1230/earmark before jedwards1230/earmark#200. The stateless handler
// serves it. The session id is forwarded when the server issues one, so a
// stateful handler fails here for the real reason, not for a missing session.
func TestHTTPLegacyCallWithProtocolMetaIsAccepted(t *testing.T) {
	ts := newProtocolTestServer(t)

	initReply := postMCP(t, ts, mcpCall{
		method: "initialize",
		params: map[string]any{
			"protocolVersion": legacyProtocol,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "contextforge-stand-in", "version": "0.0.0"},
		},
	})
	initResult := initReply.resultOf(t, "initialize@"+legacyProtocol)
	if got := initResult["protocolVersion"]; got != legacyProtocol {
		t.Fatalf("initialize negotiated protocolVersion = %#v, want %q", got, legacyProtocol)
	}

	header := http.Header{"Mcp-Protocol-Version": {legacyProtocol}}
	if sid := initReply.header.Get("Mcp-Session-Id"); sid != "" {
		header.Set("Mcp-Session-Id", sid)
	}

	initialized := postMCP(t, ts, mcpCall{method: "notifications/initialized", notification: true, header: header})
	if initialized.status != http.StatusAccepted {
		t.Fatalf("notifications/initialized: status = %d, want 202 (body: %s)", initialized.status, initialized.raw)
	}

	call := postMCP(t, ts, mcpCall{
		method: "tools/call",
		params: map[string]any{
			"name":      "testsvc_ping",
			"arguments": map[string]any{},
			"_meta":     map[string]any{mcp.MetaKeyProtocolVersion: legacyProtocol},
		},
		header: header,
	})
	assertPingResult(t, call.resultOf(t, "tools/call@"+legacyProtocol+" with _meta protocolVersion"))
}

// TestHTTPHandlerIsStateless pins the transport posture the docs promise:
// `initialize` issues no Mcp-Session-Id, and GET/DELETE /mcp answer 405 with
// `Allow: POST`. It is separate from the legacy-flow test above, which forwards
// a session id when one is issued so that a stateful handler fails there on
// -32022 rather than on this check.
func TestHTTPHandlerIsStateless(t *testing.T) {
	ts := newProtocolTestServer(t)

	t.Run("initialize issues no Mcp-Session-Id", func(t *testing.T) {
		reply := postMCP(t, ts, mcpCall{
			method: "initialize",
			params: map[string]any{
				"protocolVersion": legacyProtocol,
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "anyctl-protocol-test", "version": "0.0.0"},
			},
		})
		reply.resultOf(t, "initialize@"+legacyProtocol)
		if sid := reply.header.Get("Mcp-Session-Id"); sid != "" {
			t.Fatalf("initialize issued Mcp-Session-Id %q, want none from a stateless handler", sid)
		}
	})

	// ContextForge keeps its upstream session across an anyctl-mcp rollout, so
	// the first calls after a redeploy carry a session id the old stateful pod
	// issued. A stateful handler 404s an unknown id; the stateless one ignores it.
	t.Run("stale Mcp-Session-Id is ignored", func(t *testing.T) {
		call := postMCP(t, ts, mcpCall{
			method: "tools/call",
			params: map[string]any{"name": "testsvc_ping", "arguments": map[string]any{}},
			header: http.Header{
				"Mcp-Protocol-Version": {legacyProtocol},
				"Mcp-Session-Id":       {"STALE-SESSION-FROM-OLD-POD"},
			},
		})
		assertPingResult(t, call.resultOf(t, "tools/call with a stale Mcp-Session-Id"))
	})

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method+" /mcp answers 405", func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), method, ts.URL+"/mcp", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("%s /mcp: %v", method, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("%s /mcp: status = %d, want 405", method, resp.StatusCode)
			}
			if allow := resp.Header.Get("Allow"); allow != http.MethodPost {
				t.Fatalf("%s /mcp: Allow = %q, want %q", method, allow, http.MethodPost)
			}
		})
	}
}

// TestHTTPModernProtocolIsServed drives the sessionless 2026-07-28 revision:
// no initialize, the MCP-Protocol-Version header matched by the per-request
// `_meta` triple, plus the Mcp-Method / Mcp-Name headers that revision
// requires. A stateful handler both hides 2026-07-28 from server/discover (a
// silent downgrade — the reason ContextForge falls back to 2025-11-25) and
// rejects the call with -32022.
func TestHTTPModernProtocolIsServed(t *testing.T) {
	ts := newProtocolTestServer(t)

	modern := func(method string, params map[string]any) mcpCall {
		params["_meta"] = map[string]any{
			mcp.MetaKeyProtocolVersion:    modernProtocol,
			mcp.MetaKeyClientCapabilities: map[string]any{},
			mcp.MetaKeyClientInfo:         map[string]any{"name": "anyctl-protocol-test", "version": "0.0.0"},
		}
		h := http.Header{
			"Mcp-Protocol-Version": {modernProtocol},
			"Mcp-Method":           {method},
		}
		if name, ok := params["name"].(string); ok {
			h.Set("Mcp-Name", name)
		}
		return mcpCall{method: method, params: params, header: h}
	}

	t.Run("server/discover advertises "+modernProtocol, func(t *testing.T) {
		result := postMCP(t, ts, modern("server/discover", map[string]any{})).resultOf(t, "server/discover")
		raw, _ := result["supportedVersions"].([]any)
		var versions []string
		for _, v := range raw {
			if s, ok := v.(string); ok {
				versions = append(versions, s)
			}
		}
		if !slices.Contains(versions, modernProtocol) {
			t.Fatalf("server/discover supportedVersions = %v, want it to include %q (a stateful handler withholds it)", versions, modernProtocol)
		}
	})

	t.Run("tools/call", func(t *testing.T) {
		call := postMCP(t, ts, modern("tools/call", map[string]any{"name": "testsvc_ping", "arguments": map[string]any{}}))
		assertPingResult(t, call.resultOf(t, "tools/call@"+modernProtocol))
	})
}
