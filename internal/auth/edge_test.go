package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/piotrwolkowski/tcli/config"
)

func TestCacheRoundTrip(t *testing.T) {
	setupEnv(t, "http://unused")
	exp := time.Now().Add(time.Hour).Round(time.Second)
	in := &TokenCache{AccessToken: "at", RefreshToken: "rt", ExpiresAt: exp}
	if err := SaveCache(in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadCache()
	if err != nil || out == nil {
		t.Fatalf("LoadCache: %v, %v", out, err)
	}
	if out.AccessToken != "at" || out.RefreshToken != "rt" || !out.ExpiresAt.Equal(exp) {
		t.Errorf("got %+v, want %+v", out, in)
	}
}

func TestSaveCacheModeAndDir(t *testing.T) {
	setupEnv(t, "http://unused")
	if err := SaveCache(expiredCache()); err != nil {
		t.Fatal(err)
	}
	dir, _ := config.Dir()
	fi, err := os.Stat(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil || !di.IsDir() {
		t.Fatalf("cache dir not created: %v", err)
	}
	if di.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %v, want 0700", di.Mode().Perm())
	}
}

func TestLoadCacheMissing(t *testing.T) {
	setupEnv(t, "http://unused")
	c, err := LoadCache()
	if c != nil || err != nil {
		t.Errorf("got %v, %v; want nil, nil", c, err)
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	setupEnv(t, "http://unused")
	dir, _ := config.Dir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("{garbage"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCache()
	if err == nil || !strings.Contains(err.Error(), "parsing token cache") {
		t.Errorf("got %v", err)
	}
}

func TestClearCache(t *testing.T) {
	setupEnv(t, "http://unused")
	saveTestCache(t, expiredCache())
	if err := ClearCache(); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadCache(); c != nil || err != nil {
		t.Errorf("after clear: %v, %v", c, err)
	}
	if err := ClearCache(); err != nil {
		t.Errorf("second ClearCache: %v", err)
	}
}

func TestIsExpiredZeroTime(t *testing.T) {
	if !(&TokenCache{}).IsExpired() {
		t.Error("zero ExpiresAt should be expired")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter", "abc", 5, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"longer", "abcdefg", 5, "abcde..."},
		{"empty", "", 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.s, tt.n); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	s := "ééééé" // 5 runes, 10 bytes
	got := truncate(s, 3)
	if !utf8.ValidString(got) {
		t.Errorf("truncate(%q, 3) = %q is invalid UTF-8", s, got)
	}
}

func tokenServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPostToken(t *testing.T) {
	t.Run("200 valid", func(t *testing.T) {
		srv := tokenServer(t, 200, `{"access_token":"a","refresh_token":"r","expires_in":60}`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err != nil || tok.AccessToken != "a" || tok.RefreshToken != "r" || tok.ExpiresIn != 60 {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("200 invalid JSON", func(t *testing.T) {
		srv := tokenServer(t, 200, `not json`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err == nil || !strings.Contains(err.Error(), "parsing token response") || tok != nil {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("400 JSON error", func(t *testing.T) {
		srv := tokenServer(t, 400, `{"error":"invalid_grant","error_description":"bad"}`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err != nil || tok == nil || tok.Error != "invalid_grant" || tok.ErrorDesc != "bad" {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("400 non-JSON", func(t *testing.T) {
		srv := tokenServer(t, 400, `<html>bad</html>`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err == nil || !strings.Contains(err.Error(), "HTTP 400") || tok != nil {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("400 JSON without error field", func(t *testing.T) {
		srv := tokenServer(t, 400, `{}`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err == nil || tok != nil {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("500 JSON error is an error", func(t *testing.T) {
		srv := tokenServer(t, 500, `{"error":"server_error"}`)
		setupEnv(t, srv.URL)
		tok, err := postToken("test-tenant", url.Values{})
		if err == nil || tok != nil || !strings.Contains(err.Error(), "HTTP 500") {
			t.Errorf("got %+v, %v", tok, err)
		}
	})
	t.Run("long body truncated", func(t *testing.T) {
		srv := tokenServer(t, 502, strings.Repeat("x", 1000))
		setupEnv(t, srv.URL)
		_, err := postToken("test-tenant", url.Values{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.HasSuffix(err.Error(), strings.Repeat("x", 200)+"...") || strings.Contains(err.Error(), strings.Repeat("x", 201)) {
			t.Errorf("message not truncated to 200 chars: %d bytes", len(err.Error()))
		}
	})
}

func TestGetTokenEmptyRefreshNoHTTP(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, &TokenCache{AccessToken: "at", ExpiresAt: time.Now().Add(-time.Hour)})

	_, err := GetToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Errorf("got %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Errorf("expected no HTTP calls, got %d", n)
	}
}

func TestGetTokenKeepsOldRefreshToken(t *testing.T) {
	srv := tokenServer(t, 200, `{"access_token":"new-at","expires_in":3600}`)
	setupEnv(t, srv.URL)
	saveTestCache(t, expiredCache())

	tok, err := GetToken(context.Background())
	if err != nil || tok != "new-at" {
		t.Fatalf("got %q, %v", tok, err)
	}
	c, err := LoadCache()
	if err != nil || c == nil {
		t.Fatal(err)
	}
	if c.RefreshToken != "old-rt" || c.AccessToken != "new-at" {
		t.Errorf("cache = %+v", c)
	}
}

func TestGetTokenRefreshRequestForm(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.Write([]byte(`{"access_token":"a","expires_in":3600}`))
	}))
	defer srv.Close()
	setupEnv(t, srv.URL)
	saveTestCache(t, expiredCache())

	if _, err := GetToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if form.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q", form.Get("grant_type"))
	}
	if form.Get("client_id") != "test-client" {
		t.Errorf("client_id = %q", form.Get("client_id"))
	}
	scope := form.Get("scope")
	for _, want := range []string{"Files.ReadWrite", "offline_access"} {
		if !strings.Contains(scope, want) {
			t.Errorf("scope %q missing %q", scope, want)
		}
	}
}

func TestGetTokenConfigError(t *testing.T) {
	setupEnv(t, "http://unused")
	saveTestCache(t, expiredCache())
	t.Setenv("TCLI_CLIENT_ID", "")
	t.Setenv("TCLI_TENANT_ID", "")

	_, err := GetToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("got %v", err)
	}
}

// loginServer serves the device-code endpoint with dc and the token endpoint
// with successive entries of tokenBodies (the last one repeats).
func loginServer(t *testing.T, dcStatus int, dc string, tokenBodies ...string) *httptest.Server {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/devicecode"):
			w.WriteHeader(dcStatus)
			w.Write([]byte(dc))
		case strings.HasSuffix(r.URL.Path, "/token"):
			i := int(atomic.AddInt32(&n, 1)) - 1
			if i >= len(tokenBodies) {
				i = len(tokenBodies) - 1
			}
			if strings.Contains(tokenBodies[i], "access_token") {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(400)
			}
			w.Write([]byte(tokenBodies[i]))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const goodDC = `{"device_code":"dc","user_code":"U","verification_uri":"http://v","expires_in":60,"interval":1,"message":"go sign in"}`

func TestLoginPendingThenSuccess(t *testing.T) {
	srv := loginServer(t, 200, goodDC,
		`{"error":"authorization_pending"}`,
		`{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	setupEnv(t, srv.URL)

	if err := Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	c, err := LoadCache()
	if err != nil || c == nil {
		t.Fatalf("cache: %v, %v", c, err)
	}
	if c.AccessToken != "at" || c.RefreshToken != "rt" || c.IsExpired() {
		t.Errorf("cache = %+v", c)
	}
}

func TestLoginDeclined(t *testing.T) {
	srv := loginServer(t, 200, goodDC, `{"error":"authorization_declined"}`)
	setupEnv(t, srv.URL)
	err := Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Errorf("got %v", err)
	}
	if c, _ := LoadCache(); c != nil {
		t.Errorf("cache should not be saved: %+v", c)
	}
}

func TestLoginExpiredToken(t *testing.T) {
	srv := loginServer(t, 200, goodDC, `{"error":"expired_token"}`)
	setupEnv(t, srv.URL)
	err := Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("got %v", err)
	}
}

func TestLoginDeviceCodeError(t *testing.T) {
	srv := loginServer(t, 400, `{"error":"invalid_client","error_description":"AADSTS700016: bad app"}`)
	setupEnv(t, srv.URL)
	err := Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "AADSTS700016: bad app") {
		t.Errorf("got %v", err)
	}
}

func TestLoginDeviceCodeMissing(t *testing.T) {
	srv := loginServer(t, 200, `{"user_code":"U"}`)
	setupEnv(t, srv.URL)
	err := Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected device code response") {
		t.Errorf("got %v", err)
	}
}

func TestLoginDeviceCodeNotJSON(t *testing.T) {
	srv := loginServer(t, 200, `<html>`)
	setupEnv(t, srv.URL)
	err := Login(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parsing device code response") {
		t.Errorf("got %v", err)
	}
}

func TestLoginContextCancelled(t *testing.T) {
	srv := loginServer(t, 200, goodDC, `{"error":"authorization_pending"}`)
	setupEnv(t, srv.URL)
	pollUnit = 10 * time.Second // long enough that waiting out the interval fails the check below
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := Login(ctx)
	if err != context.Canceled {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("took %v; should not wait the poll interval", d)
	}
}

func TestLoginConfigError(t *testing.T) {
	setupEnv(t, "http://unused")
	t.Setenv("TCLI_CLIENT_ID", "")
	t.Setenv("TCLI_TENANT_ID", "")
	if err := Login(context.Background()); err == nil {
		t.Error("expected config error")
	}
}
