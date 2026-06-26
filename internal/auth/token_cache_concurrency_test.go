package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jedwards1230/labctl/internal/manifest"
	"github.com/jedwards1230/labctl/internal/template"
)

// TestFetchOAuth2TokenColdCacheHerd fires N goroutines at a COLD cache dir (no
// prime). The endpoint may be hit 1..N times — what matters is that every caller
// gets the same token, there is no data race (run under -race), and the final
// cache file is valid JSON (the temp-file+rename write is never observed torn).
func TestFetchOAuth2TokenColdCacheHerd(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"herd-tok","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	dir := t.TempDir() // cold — nothing primed
	a := manifest.Auth{
		Strategy: "oauth2-client-credentials",
		Value:    srv.URL,
		Username: "herd-client-id",
		Password: "herd-client-secret",
	}
	env := template.Env{}

	const n = 24
	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = fetchOAuth2Token(context.Background(), a, env, dir)
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if tokens[i] != "herd-tok" {
			t.Fatalf("goroutine %d token = %q, want herd-tok", i, tokens[i])
		}
	}
	if c := calls.Load(); c < 1 || c > n {
		t.Fatalf("token endpoint hit %d times, want between 1 and %d", c, n)
	}

	// The final cache file must be intact, parseable JSON (never a torn write).
	cachePath := cacheFileName(dir, "herd-client-id", srv.URL, "")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache file %s: %v", filepath.Base(cachePath), err)
	}
	var entry tokenCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("cache file is not valid JSON: %v (%s)", err, data)
	}
	if entry.AccessToken != "herd-tok" {
		t.Fatalf("cached token = %q, want herd-tok", entry.AccessToken)
	}
}

// TestFetchOAuth2TokenConcurrentCacheReads fires many parallel fetches against a
// single token server sharing one temp cache dir. The cache is primed by an
// initial serial fetch, so the subsequent N goroutines must all be served from
// the on-disk cache: exactly one token-endpoint hit total, identical tokens, and
// no data race (run under -race). This exercises the concurrent read path and
// the temp-file+rename write that produced the cache file.
func TestFetchOAuth2TokenConcurrentCacheReads(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"shared-tok","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := manifest.Auth{
		Strategy: "oauth2-client-credentials",
		Value:    srv.URL,
		Username: "shared-client-id",
		Password: "shared-client-secret",
	}
	env := template.Env{}

	// Prime the cache with one serial fetch (the only token-endpoint hit).
	primed, err := fetchOAuth2Token(context.Background(), a, env, dir)
	if err != nil {
		t.Fatalf("prime fetch: %v", err)
	}
	if primed != "shared-tok" {
		t.Fatalf("primed token = %q, want shared-tok", primed)
	}

	const n = 24
	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			tok, err := fetchOAuth2Token(context.Background(), a, env, dir)
			tokens[i], errs[i] = tok, err
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if tokens[i] != "shared-tok" {
			t.Fatalf("goroutine %d token = %q, want shared-tok", i, tokens[i])
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("token endpoint hit %d times, want 1 (warm cache must serve all %d parallel reads)", got, n)
	}
}
