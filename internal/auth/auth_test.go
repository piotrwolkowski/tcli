package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// setupEnv isolates the config dir and points the auth endpoints at srvURL.
func setupEnv(t *testing.T, srvURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TCLI_CLIENT_ID", "test-client")
	t.Setenv("TCLI_TENANT_ID", "test-tenant")
	orig := authorityHost
	authorityHost = srvURL
	t.Cleanup(func() { authorityHost = orig })
}

func saveTestCache(t *testing.T, c *TokenCache) {
	t.Helper()
	if err := SaveCache(c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
}

func expiredCache() *TokenCache {
	return &TokenCache{AccessToken: "old-at", RefreshToken: "old-rt", ExpiresAt: time.Now().Add(-time.Hour)}
}

func TestGetTokenUnexpiredNoHTTP(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, &TokenCache{AccessToken: "cached", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	tok, err := GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "cached" {
		t.Errorf("got %q, want %q", tok, "cached")
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Errorf("expected no HTTP calls, got %d", n)
	}
}

func TestGetTokenRefreshSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test-tenant/oauth2/v2.0/token" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-rt" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-at","refresh_token":"new-rt","expires_in":3600}`))
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, expiredCache())

	tok, err := GetToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "new-at" {
		t.Errorf("got %q, want %q", tok, "new-at")
	}

	c, err := LoadCache()
	if err != nil || c == nil {
		t.Fatalf("LoadCache: %v, %v", c, err)
	}
	if c.AccessToken != "new-at" || c.RefreshToken != "new-rt" {
		t.Errorf("cache not updated: %+v", c)
	}
	if c.IsExpired() {
		t.Errorf("refreshed cache should not be expired: %v", c.ExpiresAt)
	}
}

func TestGetTokenInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"AADSTS70008: expired"}`))
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, expiredCache())

	_, err := GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tcli login") {
		t.Errorf("error should direct to login, got: %v", err)
	}
}

func TestGetTokenServerUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	setupEnv(t, url)
	saveTestCache(t, expiredCache())

	_, err := GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "tcli login") {
		t.Errorf("network error should not direct to login, got: %v", err)
	}
}

func TestGetTokenServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("<html>unavailable</html>"))
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, expiredCache())

	_, err := GetToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "tcli login") || !strings.Contains(err.Error(), "503") {
		t.Errorf("want non-login error with status code, got: %v", err)
	}
}

func TestGetTokenNotLoggedIn(t *testing.T) {
	setupEnv(t, "http://127.0.0.1:0")

	_, err := GetToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("want 'not logged in' error, got: %v", err)
	}
}
