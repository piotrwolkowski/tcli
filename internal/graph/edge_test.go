package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

// --- parseGraphError ----------------------------------------------------

func TestParseGraphErrorEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"500 with JSON body", 500, `{"error":{"code":"InternalServerError","message":"boom"}}`, "Graph API error (InternalServerError): boom"},
		{"400 with empty body", 400, "", "Graph API error 400: "},
		{"JSON with empty code uses raw body", 500, `{"error":{"code":"","message":"m"}}`, `Graph API error 500: {"error":{"code":"","message":"m"}}`},
		{"403 JSON with empty code omits detail", 403, `{"error":{"code":"","message":"m"}}`, "permission denied — check"},
		{"404 JSON with empty code omits detail", 404, `{"error":{"code":"","message":"m"}}`, "not found — check"},
		{"JSON of wrong shape", 502, `[1,2]`, "Graph API error 502: [1,2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseGraphError(makeResp(tt.status, tt.body))
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Errorf("err = %v, want prefix %q", err, tt.want)
			}
		})
	}
}

// --- decodeJSON / getJSON ----------------------------------------------

func TestDecodeJSONInvalid(t *testing.T) {
	for _, body := range []string{"", "not json", `{"id":`, "<html></html>"} {
		var v Me
		err := decodeJSON(makeResp(200, body), &v)
		if err == nil || !strings.Contains(err.Error(), "parsing response") {
			t.Errorf("body %q: err = %v, want parsing response", body, err)
		}
	}
}

func TestGetJSONInvalidJSON(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{nope")
	})
	_, err := c.Me(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parsing response") {
		t.Fatalf("err = %v, want parsing response", err)
	}
}

// --- resolve ------------------------------------------------------------

func TestResolve(t *testing.T) {
	c := &Client{baseURL: "https://base/v1.0"}
	tests := map[string]string{
		"/me":                         "https://base/v1.0/me",
		"https://other.example/x?y=1": "https://other.example/x?y=1",
		"http://other.example/x":      "http://other.example/x",
		"HTTPS://upper.example/x":     "https://base/v1.0HTTPS://upper.example/x", // prefix match is case-sensitive
		"/chats/https://not-absolute": "https://base/v1.0/chats/https://not-absolute",
		"":                            "https://base/v1.0",
	}
	for in, want := range tests {
		if got := c.resolve(in); got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- send / do ----------------------------------------------------------

// cancelAfterResponse cancels a context once the round trip has completed, so
// the cancellation lands deterministically while send waits to retry.
type cancelAfterResponse struct {
	rt     http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelAfterResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.rt.RoundTrip(r)
	c.cancel()
	return resp, err
}

func TestSendContextCancelledWhileWaitingToRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "30") // within maxRetryWait, so send waits instead of failing fast
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.http = &http.Client{Transport: cancelAfterResponse{rt: http.DefaultTransport, cancel: cancel}}
	_, err := c.Me(ctx)
	if err != context.Canceled {
		t.Fatalf("err = %#v, want ctx.Err() (context.Canceled) returned unwrapped", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestSendContextDeadlineWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30") // within maxRetryWait, so send waits instead of failing fast
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := c.Me(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

func TestDoTokenError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	tokErr := errors.New("token boom")
	c.token = func(context.Context) (string, error) { return "", tokErr }

	if _, err := c.Me(context.Background()); err != tokErr {
		t.Fatalf("err = %v, want the token error unchanged", err)
	}
	if _, err := c.SendMessage(context.Background(), "chat", "hi", false); err != tokErr {
		t.Fatalf("SendMessage err = %v, want the token error unchanged", err)
	}
	if calls.Load() != 0 {
		t.Errorf("HTTP calls = %d, want 0", calls.Load())
	}
}

func TestDoBodyReadError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	_, err := c.do(context.Background(), "POST", "/x", iotest.ErrReader(errors.New("disk gone")))
	if err == nil || !strings.Contains(err.Error(), "reading request body") {
		t.Fatalf("err = %v, want reading request body", err)
	}
	if calls.Load() != 0 {
		t.Errorf("HTTP calls = %d, want 0", calls.Load())
	}
}

func TestDoNilBodySendsNoBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if len(b) != 0 {
			t.Errorf("body = %q, want empty", b)
		}
		if r.ContentLength > 0 {
			t.Errorf("ContentLength = %d, want 0", r.ContentLength)
		}
		writeJSON(t, w, map[string]any{"id": "me"})
	})
	if _, err := c.Me(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSendHeadersExact(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Range"); got != "bytes 0-3/4" {
			t.Errorf("Content-Range = %q", got)
		}
		if got := r.Header.Get("X-Weird"); got != " spaced  value; a=b,c " && got != "spaced  value; a=b,c" {
			t.Errorf("X-Weird = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none (send must not add auth)", got)
		}
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q, want none", got)
		}
		if got := r.Header.Values("X-Multi"); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("X-Multi = %v", got)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != "data" {
			t.Errorf("body = %q", b)
		}
	})
	resp, err := c.send(context.Background(), "PUT", srv.URL+"/x", []byte("data"), http.Header{
		"Content-Range": {"bytes 0-3/4"},
		"X-Weird":       {"spaced  value; a=b,c"},
		"X-Multi":       {"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestSendEmptyNonNilBodyVsNil(t *testing.T) {
	var lens []int64
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		lens = append(lens, r.ContentLength)
	})
	for _, body := range [][]byte{nil, {}} {
		resp, err := c.send(context.Background(), "POST", srv.URL+"/x", body, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if len(lens) != 2 || lens[0] != 0 || lens[1] != 0 {
		t.Errorf("content lengths = %v, want [0 0]", lens)
	}
}

func TestSendBadURL(t *testing.T) {
	c := &Client{http: &http.Client{}}
	_, err := c.send(context.Background(), "GET", "http://bad host/\x7f", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "creating request") {
		t.Fatalf("err = %v, want creating request", err)
	}
}

func TestSendTransportError(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	url := srv.URL
	srv.Close()
	_, err := c.send(context.Background(), "GET", url, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("err = %v, want request failed", err)
	}
}

// TestSendRetryBehaviour pins which responses send retries: 429 for every
// method, 503/504 only for requests that are safe to repeat (GET and PUT), and
// nothing else. Retry-After is always 0 so no case sleeps.
func TestSendRetryBehaviour(t *testing.T) {
	const (
		postBody = `{"body":{"content":"hi"}}`
		putBody  = "chunk-bytes"
	)
	tests := []struct {
		name      string
		method    string
		statuses  []int // status per call; the last one repeats
		wantCalls int
		wantErr   string // empty means success
	}{
		{"GET 503 then 200", "GET", []int{503, 200}, 2, ""},
		{"GET 504 then 200", "GET", []int{504, 200}, 2, ""},
		{"PUT 503 then 202", "PUT", []int{503, 202}, 2, ""},
		{"PUT 504 then 202", "PUT", []int{504, 202}, 2, ""},
		{"GET 429 then 200", "GET", []int{429, 200}, 2, ""},
		{"POST 429 then 201", "POST", []int{429, 201}, 2, ""},
		{"PUT 429 then 202", "PUT", []int{429, 202}, 2, ""},
		{"GET 429 twice then 200", "GET", []int{429, 429, 200}, 3, ""},

		{"POST 503 is not retried", "POST", []int{503, 200}, 1, "Graph API unavailable (HTTP 503) — try again later"},
		{"POST 504 is not retried and warns it may have gone through", "POST", []int{504, 200}, 1, "check the chat before retrying"},

		{"GET 503 exhausts retries", "GET", []int{503}, maxRetries + 1, "Graph API unavailable (HTTP 503) — try again later"},
		{"GET 504 exhausts retries", "GET", []int{504}, maxRetries + 1, "Graph API unavailable (HTTP 504) — try again later"},
		{"PUT 503 exhausts retries", "PUT", []int{503}, maxRetries + 1, "Graph API unavailable (HTTP 503) — try again later"},
		{"GET 429 exhausts retries", "GET", []int{429}, maxRetries + 1, "rate limited by Graph API — try again later"},
		{"POST 429 exhausts retries", "POST", []int{429}, maxRetries + 1, "rate limited by Graph API — try again later"},
	}
	// Other errors are final for every method, including PUT.
	for _, tc := range []struct {
		status  int
		wantErr string
	}{
		{400, "Graph API error 400"},
		{401, "unauthorized"},
		{403, "permission denied"},
		{404, "not found"},
		{500, "Graph API error 500"},
	} {
		for _, method := range []string{"GET", "POST", "PUT"} {
			tests = append(tests, struct {
				name      string
				method    string
				statuses  []int
				wantCalls int
				wantErr   string
			}{fmt.Sprintf("%s %d is not retried", method, tc.status), method, []int{tc.status, 200}, 1, tc.wantErr})
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantBody := ""
			switch tt.method {
			case "POST":
				wantBody = postBody
			case "PUT":
				wantBody = putBody
			}

			var calls atomic.Int32
			c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				i := int(calls.Add(1)) - 1
				// The body must be replayed in full on every attempt.
				if b, _ := io.ReadAll(r.Body); string(b) != wantBody {
					t.Errorf("call %d: body = %q, want %q", i+1, b, wantBody)
				}
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tt.statuses[min(i, len(tt.statuses)-1)])
			})

			var resp *http.Response
			var err error
			switch tt.method {
			case "PUT": // upload-chunk style: absolute URL, no auth, via send
				resp, err = c.send(context.Background(), "PUT", srv.URL+"/chunk", []byte(putBody), nil)
			case "POST":
				resp, err = c.do(context.Background(), "POST", "/x", strings.NewReader(postBody))
			default:
				resp, err = c.do(context.Background(), tt.method, "/x", nil)
			}
			if err == nil {
				resp.Body.Close()
			}

			if got := int(calls.Load()); got != tt.wantCalls {
				t.Errorf("calls = %d, want %d", got, tt.wantCalls)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want success", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			// A final (non-retry) error must not claim the request will work later.
			if !strings.Contains(tt.wantErr, "try again later") && strings.Contains(err.Error(), "try again later") {
				t.Errorf("err = %q, must not suggest retrying", err)
			}
		})
	}
}

// When Graph asks for a wait longer than maxRetryWait, send must fail at once
// (one call, no sleeping) and tell the user how long to wait.
func TestSendFailsFastWhenRetryAfterTooLong(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		status     int
		retryAfter string
		wantErr    string
	}{
		{"429 Retry-After 120", "GET", 429, "120", "rate limited by Graph API — retry in 2m0s"},
		{"POST 429 Retry-After 120", "POST", 429, "120", "rate limited by Graph API — retry in 2m0s"},
		{"GET 503 Retry-After 61", "GET", 503, "61", "Graph API unavailable (HTTP 503) — retry in 1m1s"},
		{"GET 504 Retry-After 3600", "GET", 504, "3600", "Graph API unavailable (HTTP 504) — retry in 1h0m0s"},
		{"429 Retry-After 1e10 is capped, not overflowed", "GET", 429, "10000000000", "rate limited by Graph API — retry in "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The timeout only bounds a regression to the old sleeping behaviour.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var calls atomic.Int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", tt.retryAfter)
				w.WriteHeader(tt.status)
			})

			start := time.Now()
			resp, err := c.do(ctx, tt.method, "/x", nil)
			elapsed := time.Since(start)
			if err == nil {
				resp.Body.Close()
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %q, want containing %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "try again later") {
				t.Errorf("err = %q, want a retry-in duration instead of try again later", err)
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("calls = %d, want 1", got)
			}
			if elapsed >= time.Second {
				t.Errorf("took %v, want an immediate failure", elapsed)
			}
		})
	}
}

// A 503 on a POST is not retried, so even a long Retry-After must not turn it
// into a "retry in" message: the generic error is returned immediately.
func TestSendPostUnavailableIgnoresRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err := c.do(context.Background(), "POST", "/x", strings.NewReader("{}"))
	if err == nil || !strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "retry in") {
		t.Fatalf("err = %v, want a plain 503 error without a retry hint", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// retryable is the single gate for retrying; pin its full truth table.
func TestRetryable(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}
	for _, method := range methods {
		for _, status := range []int{200, 201, 204, 302, 400, 401, 403, 404, 408, 409, 429, 500, 501, 502, 503, 504} {
			want := status == 429 ||
				((status == 503 || status == 504) && (method == "GET" || method == "PUT"))
			if got := retryable(method, status); got != want {
				t.Errorf("retryable(%s, %d) = %v, want %v", method, status, got, want)
			}
		}
	}
}

// --- createdBefore ------------------------------------------------------

func TestCreatedBefore(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"equal times", "2026-01-01T10:00:00Z", "2026-01-01T10:00:00Z", false},
		{"equal instants, different offsets", "2026-01-01T12:00:00+02:00", "2026-01-01T10:00:00Z", false},
		{"fractional seconds", "2026-01-01T10:00:00.1Z", "2026-01-01T10:00:00.2Z", true},
		{"a unparseable, string compare", "zzz", "2026-01-01T10:00:00Z", false},
		{"a unparseable, string compare (less)", "", "2026-01-01T10:00:00Z", true},
		{"b unparseable, string compare", "2026-01-01T10:00:00Z", "zzz", true},
		{"both unparseable", "a", "b", true},
		{"both unparseable equal", "a", "a", false},
		{"both unparseable reversed", "b", "a", false},
		{"both empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createdBefore(Message{CreatedDateTime: tt.a}, Message{CreatedDateTime: tt.b})
			if got != tt.want {
				t.Errorf("createdBefore(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// --- ListMessagesAfterMine ---------------------------------------------

func TestListMessagesAfterMineMaxPagesNoCalls(t *testing.T) {
	for _, maxPages := range []int{0, -1} {
		var calls atomic.Int32
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
		got, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", maxPages)
		if err != nil || len(got) != 0 {
			t.Errorf("maxPages=%d: got %v, err %v", maxPages, got, err)
		}
		if calls.Load() != 0 {
			t.Errorf("maxPages=%d: calls = %d, want 0", maxPages, calls.Load())
		}
	}
}

func TestListMessagesAfterMineNilSenders(t *testing.T) {
	botNilFrom := msg("bot1", "x", "2026-01-01T10:04:00Z")
	botNilFrom["from"] = nil
	appOnly := msg("app", "x", "2026-01-01T10:03:00Z")
	appOnly["from"] = map[string]any{"application": map[string]any{"id": "app1"}} // user absent
	userNull := msg("usernull", "x", "2026-01-01T10:02:00Z")
	userNull["from"] = map[string]any{"user": nil}

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"value": []map[string]any{
			botNilFrom, appOnly, userNull,
			msg("mine", "me", "2026-01-01T10:01:00Z"),
			msg("old", "bob", "2026-01-01T10:00:00Z"),
		}})
	})
	got, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", 3)
	if err != nil {
		t.Fatal(err)
	}
	if g, w := strings.Join(ids(got), ","), "usernull,app,bot1"; g != w {
		t.Errorf("ids = %s, want %s", g, w)
	}
}

func TestListMessagesAfterMineEmptyMeIDDoesNotMatchNilUsers(t *testing.T) {
	// A sender whose user ID is "" must not be mistaken for "me" when meID is
	// also empty only if user is non-nil; nil users never match.
	m := msg("a", "bob", "2026-01-01T10:00:00Z")
	m["from"] = nil
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"value": []map[string]any{m}})
	})
	got, err := c.ListMessagesAfterMine(context.Background(), "chat", "", 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestListMessagesAfterMineErrorOnPage2(t *testing.T) {
	var srvURL string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writeJSON(t, w, map[string]any{
			"value":           []map[string]any{msg("a", "bob", "2026-01-01T10:00:00Z")},
			"@odata.nextLink": srvURL + "/next?page=2",
		})
	})
	srvURL = srv.URL
	got, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", 5)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil on error", got)
	}
}

func TestListMessagesAfterMineEmptyAndBadJSON(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"value": []any{}})
	})
	got, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", 5)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, err %v", got, err)
	}

	c, _ = newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "oops") })
	if _, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", 5); err == nil || !strings.Contains(err.Error(), "parsing response") {
		t.Errorf("err = %v, want parsing response", err)
	}
}

func TestListMessagesAfterMineDuplicateTimestampsStable(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"value": []map[string]any{
			msg("n3", "bob", "2026-01-01T10:00:00Z"),
			msg("n2", "bob", "2026-01-01T10:00:00Z"),
			msg("n1", "bob", "2026-01-01T10:00:00Z"),
		}})
	})
	got, err := c.ListMessagesAfterMine(context.Background(), "chat", "me", 1)
	if err != nil {
		t.Fatal(err)
	}
	// Equal timestamps keep Graph's (newest-first) order: sort is stable.
	if g, w := strings.Join(ids(got), ","), "n3,n2,n1"; g != w {
		t.Errorf("ids = %s, want %s", g, w)
	}
}

// --- ListChats / ListChatMembers ---------------------------------------

func TestListChatsAndMembersEdge(t *testing.T) {
	type lister func(c *Client) (int, error)
	listers := map[string]lister{
		"ListChats": func(c *Client) (int, error) {
			r, err := c.ListChats(context.Background())
			return len(r), err
		},
		"ListChatMembers": func(c *Client) (int, error) {
			r, err := c.ListChatMembers(context.Background(), "c1")
			return len(r), err
		},
	}
	for name, list := range listers {
		t.Run(name+"/empty value", func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, map[string]any{"value": []any{}})
			})
			n, err := list(c)
			if err != nil || n != 0 {
				t.Errorf("n = %d, err = %v", n, err)
			}
		})
		t.Run(name+"/missing value", func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
			n, err := list(c)
			if err != nil || n != 0 {
				t.Errorf("n = %d, err = %v", n, err)
			}
		})
		t.Run(name+"/error mid-pagination", func(t *testing.T) {
			var srvURL string
			c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("p") == "2" {
					w.WriteHeader(http.StatusInternalServerError)
					io.WriteString(w, `{"error":{"code":"Boom","message":"mid"}}`)
					return
				}
				writeJSON(t, w, map[string]any{
					"value":           []map[string]any{{"id": "1"}},
					"@odata.nextLink": srvURL + "/next?p=2",
				})
			})
			srvURL = srv.URL
			n, err := list(c)
			if err == nil || !strings.Contains(err.Error(), "Boom") {
				t.Fatalf("err = %v, want Boom", err)
			}
			if n != 0 {
				t.Errorf("n = %d, want 0 (no partial results on error)", n)
			}
		})
		t.Run(name+"/bad json", func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "[") })
			if _, err := list(c); err == nil || !strings.Contains(err.Error(), "parsing response") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestListChatMembersEscapesChatID(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/chats/a%2Fb%3Fc%23d%20e/members" {
			t.Errorf("path = %q", got)
		}
		writeJSON(t, w, map[string]any{"value": []any{}})
	})
	if _, err := c.ListChatMembers(context.Background(), "a/b?c#d e"); err != nil {
		t.Fatal(err)
	}
}

// --- DownloadFile / DownloadHostedContent ------------------------------

func TestDownloadFileEdge(t *testing.T) {
	nonFiles := map[string]Attachment{
		"adaptive card":         {ContentType: "application/vnd.microsoft.card.adaptive", Content: "{}", Name: "card"},
		"reference without url": {ContentType: "reference", Name: "nourl"},
		"url but not reference": {ContentType: "messageReference", ContentURL: "https://x/y", Name: "q"},
		"zero value":            {},
	}
	for name, a := range nonFiles {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
			resp, err := c.DownloadFile(context.Background(), a)
			if err == nil || !strings.Contains(err.Error(), "is not a file") {
				t.Fatalf("err = %v, want not a file", err)
			}
			if resp != nil {
				t.Errorf("resp = %v, want nil", resp)
			}
			if calls.Load() != 0 {
				t.Errorf("HTTP calls = %d, want 0", calls.Load())
			}
		})
	}

	t.Run("request path", func(t *testing.T) {
		const contentURL = "https://contoso.sharepoint.com/personal/a/Documents/Microsoft Teams Chat Files/r.pdf"
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			want := "/shares/" + encodeShareID(contentURL) + "/driveItem/content"
			if got := r.URL.EscapedPath(); got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			if !strings.HasPrefix(r.URL.Path, "/shares/u!") {
				t.Errorf("path = %q, want /shares/u! prefix", r.URL.Path)
			}
			if r.Method != "GET" {
				t.Errorf("method = %s", r.Method)
			}
			io.WriteString(w, "PDFBYTES")
		})
		resp, err := c.DownloadFile(context.Background(), Attachment{ContentType: "reference", ContentURL: contentURL})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if b, _ := io.ReadAll(resp.Body); string(b) != "PDFBYTES" {
			t.Errorf("body = %q", b)
		}
	})

	t.Run("404 mapped", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
		_, err := c.DownloadFile(context.Background(), Attachment{ContentType: "reference", ContentURL: "https://x/y"})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestDownloadHostedContentEscapesIDs(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		want := "/me/chats/19:a%2Fb@thread.v2/messages/1%3F2%23%203/hostedContents/aWQ9%2Fx==/$value"
		if got := r.URL.EscapedPath(); got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none (IDs must not leak into the query)", r.URL.RawQuery)
		}
		io.WriteString(w, "IMG")
	})
	resp, err := c.DownloadHostedContent(context.Background(), "19:a/b@thread.v2", "1?2# 3", "aWQ9/x==")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

// --- HostedContentIDs ---------------------------------------------------

func TestHostedContentIDsEdge(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"single-quoted src", `<img src='https://g/hostedContents/abc/$value'>`, []string{"abc"}},
		{"IDs with = - _", `<img src="https://g/hostedContents/aWQ9-x_y==/$value">`, []string{"aWQ9-x_y=="}},
		{"no $value", `<img src="https://g/hostedContents/x/">`, nil},
		{"no trailing segment", `<img src="https://g/hostedContents/x">`, nil},
		{"wrong suffix", `<img src="https://g/hostedContents/x/$valuez">`, []string{"x"}}, // documents prefix match on $value
		{"empty ID", `<img src="https://g/hostedContents//$value">`, nil},
		{"whitespace in ID terminates", `<img src="https://g/hostedContents/a b/$value">`, nil},
		{"order preserved, dedup", `/hostedContents/b/$value /hostedContents/a/$value /hostedContents/b/$value`, []string{"b", "a"}},
		{"empty content", ``, nil},
		{"case-sensitive", `/HostedContents/x/$value`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Message{Body: MessageBodyFull{Content: tt.content}}.HostedContentIDs()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// --- oneDriveName -------------------------------------------------------

func TestOneDriveNameEdge(t *testing.T) {
	tests := []struct{ in, want string }{
		{"résumé.pdf", "résumé.pdf"},
		{"日本語.txt", "日本語.txt"},
		{"???", "___"},
		{"  .env", ".env"},
		{"report. . ", "report"},
		{". . ", "file"},
		{"   ", "file"},
		{"...   ...", "file"},
		{"valid name (1).txt", "valid name (1).txt"},
		{"a.b.c", "a.b.c"},
		{`"*:<>?/\|`, "_________"},
		{" a b ", "a b"},
		{".hidden", ".hidden"},
		{"..a", "..a"},
		{"trailing...", "trailing"},
		{"a:", "a_"},
		{"..:", ".._"}, // ":" becomes "_" so dots before it are no longer trailing
	}
	for _, tt := range tests {
		if got := oneDriveName(tt.in); got != tt.want {
			t.Errorf("oneDriveName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- ChatAttachment -----------------------------------------------------

func TestChatAttachmentEdge(t *testing.T) {
	const g1 = "153FA47D-18C9-4179-BE08-9879815A9F90"
	const g2 = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	tests := []struct {
		name    string
		etag    string
		wantID  string
		wantErr bool
	}{
		{"GUID without braces", `"` + g1 + `,2"`, "", true},
		{"two braced GUIDs, first wins", `"{` + g1 + `},{` + g2 + `}"`, strings.ToLower(g1), false},
		{"no surrounding quotes", `{` + g1 + `},2`, strings.ToLower(g1), false},
		{"mixed case lowercased", `"{153fA47d-18C9-4179-bE08-9879815a9F90},2"`, strings.ToLower(g1), false},
		{"empty etag", ``, "", true},
		{"short GUID", `"{153FA47D-18C9},2"`, "", true},
		{"too-long GUID", `"{` + g1 + `0},2"`, "", true},
		{"non-hex GUID", `"{ZZZFA47D-18C9-4179-BE08-9879815A9F90},2"`, "", true},
		{"unclosed brace", `"{` + g1 + `,2"`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := DriveItem{Name: "f.txt", ETag: tt.etag, WebDavURL: "https://dav/f.txt"}.ChatAttachment()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if a.ID != tt.wantID || a.ContentType != "reference" || a.ContentURL != "https://dav/f.txt" || a.Name != "f.txt" {
				t.Errorf("attachment = %+v", a)
			}
		})
	}
}

// --- UploadChatFile / uploadChunks -------------------------------------

func TestUploadChatFileEdge(t *testing.T) {
	t.Run("reader error aborts with no further PUTs", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, nil)
		boom := errors.New("read boom")
		_, err := c.UploadChatFile(context.Background(), "f.txt", iotest.ErrReader(boom), 8)
		if err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "did it change") {
			t.Fatalf("err = %v, want wrapped reader error", err)
		}
		if len(us.puts) != 0 || us.getCalls != 0 {
			t.Errorf("puts = %d, gets = %d, want none", len(us.puts), us.getCalls)
		}
	})

	t.Run("reader error after first chunk stops further PUTs", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, nil)
		boom := errors.New("late boom")
		r := io.MultiReader(strings.NewReader("0123"), iotest.ErrReader(boom))
		_, err := c.UploadChatFile(context.Background(), "f.txt", r, 12)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if len(us.puts) != 1 {
			t.Errorf("puts = %d, want 1", len(us.puts))
		}
	})

	t.Run("OneByteReader still yields full chunks", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, finalCreated(t, 3))
		item, err := c.UploadChatFile(context.Background(), "f.txt", iotest.OneByteReader(strings.NewReader("0123456789")), 10)
		if err != nil {
			t.Fatal(err)
		}
		if item.ID != "item1" {
			t.Errorf("item = %+v", item)
		}
		wantBodies := []string{"0123", "4567", "89"}
		wantRanges := []string{"bytes 0-3/10", "bytes 4-7/10", "bytes 8-9/10"}
		if len(us.puts) != 3 {
			t.Fatalf("puts = %d, want 3", len(us.puts))
		}
		for i, p := range us.puts {
			if p.body != wantBodies[i] || p.rng != wantRanges[i] {
				t.Errorf("put %d = %+v, want body %q range %q", i, p, wantBodies[i], wantRanges[i])
			}
			if p.auth != "" {
				t.Errorf("put %d sent Authorization %q to the pre-authenticated URL", i, p.auth)
			}
		}
	})

	t.Run("size equals chunk size gives one PUT", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, finalCreated(t, 1))
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4); err != nil {
			t.Fatal(err)
		}
		if len(us.puts) != 1 || us.puts[0].rng != "bytes 0-3/4" {
			t.Errorf("puts = %+v", us.puts)
		}
	})

	t.Run("size one", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, finalCreated(t, 1))
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
		if len(us.puts) != 1 || us.puts[0].rng != "bytes 0-0/1" || us.puts[0].body != "x" {
			t.Errorf("puts = %+v", us.puts)
		}
	})

	t.Run("size one more than chunk size gives two PUTs", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, finalCreated(t, 2))
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcde"), 5); err != nil {
			t.Fatal(err)
		}
		if len(us.puts) != 2 || us.puts[1].rng != "bytes 4-4/5" {
			t.Errorf("puts = %+v", us.puts)
		}
	})

	t.Run("negative size errors without HTTP", func(t *testing.T) {
		c, us := newUploadServer(t, nil)
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abc"), -5)
		if err == nil {
			t.Fatal("want error")
		}
		if us.createCalls != 0 || len(us.puts) != 0 || us.getCalls != 0 {
			t.Errorf("unexpected HTTP calls: %+v", us)
		}
	})

	t.Run("reader longer than size is only read up to size", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, finalCreated(t, 1))
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcdefgh"), 4); err != nil {
			t.Fatal(err)
		}
		if len(us.puts) != 1 || us.puts[0].body != "abcd" {
			t.Errorf("puts = %+v", us.puts)
		}
	})
}

func TestUploadChatFileSessionAndSelectFailures(t *testing.T) {
	t.Run("createUploadSession 403", func(t *testing.T) {
		var puts, others atomic.Int32
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "PUT" {
				puts.Add(1)
			} else {
				others.Add(1)
			}
			w.WriteHeader(http.StatusForbidden)
		})
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("err = %v, want permission denied", err)
		}
		if puts.Load() != 0 || others.Load() != 1 {
			t.Errorf("puts = %d, others = %d, want 0 and 1", puts.Load(), others.Load())
		}
	})

	t.Run("empty uploadUrl", func(t *testing.T) {
		var calls atomic.Int32
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			writeJSON(t, w, map[string]any{"uploadUrl": ""})
		})
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4)
		if err == nil || !strings.Contains(err.Error(), "no upload URL") {
			t.Fatalf("err = %v", err)
		}
		if calls.Load() != 1 {
			t.Errorf("calls = %d, want 1", calls.Load())
		}
	})

	t.Run("missing uploadUrl field", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("session response not JSON", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "nope") })
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4)
		if err == nil || !strings.Contains(err.Error(), "parsing response") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("final select GET fails", func(t *testing.T) {
		var srvURL string
		var puts, gets atomic.Int32
		c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST":
				writeJSON(t, w, map[string]any{"uploadUrl": srvURL + "/up"})
			case r.Method == "PUT":
				puts.Add(1)
				w.WriteHeader(http.StatusCreated)
				writeJSON(t, w, map[string]any{"id": "item1"})
			default:
				gets.Add(1)
				w.WriteHeader(http.StatusNotFound)
			}
		})
		srvURL = srv.URL
		item, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("err = %v, want not found", err)
		}
		if item != nil {
			t.Errorf("item = %+v, want nil", item)
		}
		if puts.Load() != 1 || gets.Load() != 1 {
			t.Errorf("puts = %d, gets = %d", puts.Load(), gets.Load())
		}
	})

	t.Run("final select GET returns invalid JSON", func(t *testing.T) {
		var srvURL string
		c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				writeJSON(t, w, map[string]any{"uploadUrl": srvURL + "/up"})
			case "PUT":
				w.WriteHeader(http.StatusOK)
				writeJSON(t, w, map[string]any{"id": "item1"})
			default:
				io.WriteString(w, "garbage")
			}
		})
		srvURL = srv.URL
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4)
		if err == nil || !strings.Contains(err.Error(), "parsing response") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("item ID is path-escaped in select GET", func(t *testing.T) {
		var srvURL string
		c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "POST":
				writeJSON(t, w, map[string]any{"uploadUrl": srvURL + "/up"})
			case "PUT":
				w.WriteHeader(http.StatusCreated)
				writeJSON(t, w, map[string]any{"id": "a/b?c"})
			default:
				if got := r.URL.EscapedPath(); got != "/me/drive/items/a%2Fb%3Fc" {
					t.Errorf("path = %q", got)
				}
				writeJSON(t, w, map[string]any{"id": "a/b?c"})
			}
		})
		srvURL = srv.URL
		if _, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("abcd"), 4); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("PUT 500 aborts the upload", func(t *testing.T) {
		setChunkSize(t, 4)
		c, us := newUploadServer(t, func(i int, w http.ResponseWriter) { w.WriteHeader(500) })
		_, err := c.UploadChatFile(context.Background(), "f.txt", strings.NewReader("0123456789"), 10)
		if err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err = %v", err)
		}
		if len(us.puts) != 1 {
			t.Errorf("puts = %d, want 1", len(us.puts))
		}
	})
}

// --- SendMessage --------------------------------------------------------

func captureSend(t *testing.T, content string, asHTML bool, atts ...Attachment) (SendMessageRequest, string) {
	t.Helper()
	var raw string
	var req SendMessageRequest
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		if err := json.Unmarshal(b, &req); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		writeJSON(t, w, map[string]any{"id": "m1"})
	})
	if _, err := c.SendMessage(context.Background(), "chat", content, asHTML, atts...); err != nil {
		t.Fatal(err)
	}
	return req, raw
}

func TestSendMessageEdge(t *testing.T) {
	t.Run("attachment ID with quote and angle bracket is escaped", func(t *testing.T) {
		req, _ := captureSend(t, "hi", false, Attachment{ID: `a"b<c>&d`, ContentType: "reference"})
		want := `hi<attachment id="a&#34;b&lt;c&gt;&amp;d"></attachment>`
		if req.Body.Content != want {
			t.Errorf("content = %q, want %q", req.Body.Content, want)
		}
		if req.Body.ContentType != "html" {
			t.Errorf("contentType = %q, want html", req.Body.ContentType)
		}
		// The raw ID is still sent unescaped in the attachments array.
		if len(req.Attachments) != 1 || req.Attachments[0].ID != `a"b<c>&d` {
			t.Errorf("attachments = %+v", req.Attachments)
		}
	})

	t.Run("double newline becomes <br><br>", func(t *testing.T) {
		req, _ := captureSend(t, "a\n\nb", false, Attachment{ID: "x", ContentType: "reference"})
		if want := `a<br><br>b<attachment id="x"></attachment>`; req.Body.Content != want {
			t.Errorf("content = %q, want %q", req.Body.Content, want)
		}
	})

	t.Run("only newlines", func(t *testing.T) {
		req, _ := captureSend(t, "\n\n", false, Attachment{ID: "x", ContentType: "reference"})
		if want := `<br><br><attachment id="x"></attachment>`; req.Body.Content != want {
			t.Errorf("content = %q, want %q", req.Body.Content, want)
		}
	})

	t.Run("plain text is HTML-escaped when attachments present", func(t *testing.T) {
		req, _ := captureSend(t, "<b>1 & 2</b>", false, Attachment{ID: "x", ContentType: "reference"})
		if want := `&lt;b&gt;1 &amp; 2&lt;/b&gt;<attachment id="x"></attachment>`; req.Body.Content != want {
			t.Errorf("content = %q, want %q", req.Body.Content, want)
		}
	})

	t.Run("asHTML without attachments is unchanged", func(t *testing.T) {
		in := "<b>bold</b>\n<i>x</i> & \"q\""
		req, _ := captureSend(t, in, true)
		if req.Body.Content != in || req.Body.ContentType != "html" {
			t.Errorf("body = %+v", req.Body)
		}
		if req.Attachments != nil {
			t.Errorf("attachments = %v, want none", req.Attachments)
		}
	})

	t.Run("asHTML with attachments keeps HTML, appends tags in order", func(t *testing.T) {
		req, _ := captureSend(t, "<p>x\n</p>", true,
			Attachment{ID: "1", ContentType: "reference"}, Attachment{ID: "2", ContentType: "reference"})
		if want := "<p>x\n</p><attachment id=\"1\"></attachment><attachment id=\"2\"></attachment>"; req.Body.Content != want {
			t.Errorf("content = %q, want %q", req.Body.Content, want)
		}
	})

	t.Run("plain text without attachments is not escaped", func(t *testing.T) {
		req, raw := captureSend(t, "a < b\nc", false)
		if req.Body.Content != "a < b\nc" || req.Body.ContentType != "" {
			t.Errorf("body = %+v", req.Body)
		}
		if strings.Contains(raw, "attachments") {
			t.Errorf("raw = %s, want attachments omitted", raw)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		req, raw := captureSend(t, "", false)
		if req.Body.Content != "" || !strings.Contains(raw, `"content":""`) {
			t.Errorf("raw = %s", raw)
		}
	})

	t.Run("HTTP 400 propagated", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":"BadRequest","message":"bad body"}}`)
		})
		resp, err := c.SendMessage(context.Background(), "chat", "x", false)
		if err == nil || !strings.Contains(err.Error(), "BadRequest") || !strings.Contains(err.Error(), "bad body") {
			t.Fatalf("err = %v", err)
		}
		if resp != nil {
			t.Errorf("resp = %+v, want nil", resp)
		}
	})

	t.Run("invalid JSON in response", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "not json") })
		if _, err := c.SendMessage(context.Background(), "chat", "x", false); err == nil || !strings.Contains(err.Error(), "parsing response") {
			t.Fatalf("err = %v", err)
		}
	})
}

// --- ShareFile / invite -------------------------------------------------

type shareCall struct{ body inviteBody }

func shareServer(t *testing.T, status func(n int, req inviteBody) int) (*Client, *[]shareCall) {
	t.Helper()
	var calls []shareCall
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var b inviteBody
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Errorf("decoding invite: %v", err)
		}
		calls = append(calls, shareCall{b})
		code := 200
		if status != nil {
			code = status(len(calls)-1, b)
		}
		w.WriteHeader(code)
		if code >= 400 {
			io.WriteString(w, `{"error":{"code":"accessDenied","message":"no"}}`)
			return
		}
		io.WriteString(w, "{}")
	})
	return c, &calls
}

func TestShareFileEdge(t *testing.T) {
	t.Run("email and userId sends only email", func(t *testing.T) {
		c, calls := shareServer(t, nil)
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{{Email: "a@x", UserID: "u1"}})
		if err != nil || len(failed) != 0 {
			t.Fatalf("failed = %v, err = %v", failed, err)
		}
		if len(*calls) != 1 {
			t.Fatalf("calls = %d", len(*calls))
		}
		rec := (*calls)[0].body.Recipients
		if len(rec) != 1 || rec[0]["email"] != "a@x" {
			t.Fatalf("recipients = %v", rec)
		}
		if _, has := rec[0]["objectId"]; has {
			t.Errorf("recipient has objectId too: %v", rec[0])
		}
	})

	t.Run("empty email with userId uses objectId", func(t *testing.T) {
		c, calls := shareServer(t, nil)
		if _, err := c.ShareFile(context.Background(), "item", []ChatMember{{Email: "", UserID: "u1"}}); err != nil {
			t.Fatal(err)
		}
		rec := (*calls)[0].body.Recipients
		if len(rec) != 1 || rec[0]["objectId"] != "u1" {
			t.Fatalf("recipients = %v", rec)
		}
		if _, has := rec[0]["email"]; has {
			t.Errorf("recipient has email: %v", rec[0])
		}
	})

	t.Run("single failing member: one call, member returned with error", func(t *testing.T) {
		m := ChatMember{Email: "a@x", DisplayName: "A"}
		c, calls := shareServer(t, func(int, inviteBody) int { return 403 })
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{m})
		if err == nil {
			t.Fatal("want error")
		}
		if !reflect.DeepEqual(failed, []ChatMember{m}) {
			t.Errorf("failed = %v, want [%v]", failed, m)
		}
		if len(*calls) != 1 {
			t.Errorf("calls = %d, want exactly 1 (no per-member retry)", len(*calls))
		}
	})

	t.Run("single failing member plus unshareable keeps both in failed", func(t *testing.T) {
		ghost := ChatMember{DisplayName: "Ghost"}
		m := ChatMember{Email: "a@x"}
		c, calls := shareServer(t, func(int, inviteBody) int { return 500 })
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{ghost, m})
		if err == nil {
			t.Fatal("want error")
		}
		if !reflect.DeepEqual(failed, []ChatMember{ghost, m}) {
			t.Errorf("failed = %v", failed)
		}
		if len(*calls) != 1 {
			t.Errorf("calls = %d, want 1", len(*calls))
		}
	})

	t.Run("item ID is path-escaped", func(t *testing.T) {
		var path string
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.EscapedPath()
			io.WriteString(w, "{}")
		})
		if _, err := c.ShareFile(context.Background(), "a/b c", []ChatMember{{Email: "a@x"}}); err != nil {
			t.Fatal(err)
		}
		if path != "/me/drive/items/a%2Fb%20c/invite" {
			t.Errorf("path = %q", path)
		}
	})

	t.Run("body flags", func(t *testing.T) {
		c, calls := shareServer(t, nil)
		if _, err := c.ShareFile(context.Background(), "item", []ChatMember{{Email: "a@x"}}); err != nil {
			t.Fatal(err)
		}
		b := (*calls)[0].body
		if !reflect.DeepEqual(b.Roles, []string{"read"}) || !b.RequireSignIn || b.SendInvitation {
			t.Errorf("body = %+v", b)
		}
	})

	t.Run("batch fails, every individual retry succeeds returns no error", func(t *testing.T) {
		c, calls := shareServer(t, func(n int, req inviteBody) int {
			if len(req.Recipients) > 1 {
				return 400
			}
			return 200
		})
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{{Email: "a@x"}, {Email: "b@x"}})
		if err != nil || len(failed) != 0 {
			t.Errorf("failed = %v, err = %v", failed, err)
		}
		if len(*calls) != 3 {
			t.Errorf("calls = %d, want 3", len(*calls))
		}
	})

	t.Run("multiple unshareable members, no shareable", func(t *testing.T) {
		c, calls := shareServer(t, nil)
		g1, g2 := ChatMember{DisplayName: "G1"}, ChatMember{DisplayName: "G2"}
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{g1, g2})
		if err == nil || !reflect.DeepEqual(failed, []ChatMember{g1, g2}) {
			t.Errorf("failed = %v, err = %v", failed, err)
		}
		if len(*calls) != 0 {
			t.Errorf("calls = %d, want 0", len(*calls))
		}
	})

	t.Run("429 on invite is retried transparently", func(t *testing.T) {
		var n int
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
				return
			}
			io.WriteString(w, "{}")
		})
		failed, err := c.ShareFile(context.Background(), "item", []ChatMember{{Email: "a@x"}})
		if err != nil || len(failed) != 0 || n != 2 {
			t.Errorf("failed = %v, err = %v, n = %d", failed, err, n)
		}
	})
}

// --- SetReaction / UnsetReaction ---------------------------------------

func TestReactionEdge(t *testing.T) {
	emojis := map[string]string{
		"thumbs up":         "👍",
		"ZWJ sequence":      "👩‍💻",
		"flag":              "🇵🇱",
		"skin tone":         "👍🏽",
		"keycap":            "1️⃣",
		"quote and angle":   `"<&>`,
		"empty":             "",
		"legacy name":       "like",
		"non-BMP and ascii": "a😀b",
	}
	actions := map[string]func(c *Client, chat, id, emoji string) error{
		"setReaction": func(c *Client, chat, id, emoji string) error {
			return c.SetReaction(context.Background(), chat, id, emoji)
		},
		"unsetReaction": func(c *Client, chat, id, emoji string) error {
			return c.UnsetReaction(context.Background(), chat, id, emoji)
		},
	}
	for action, call := range actions {
		for name, emoji := range emojis {
			t.Run(action+"/"+name, func(t *testing.T) {
				c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					if want := "/chats/19:abc@thread.v2/messages/1%2F2%3F3%23%204/" + action; r.URL.EscapedPath() != want {
						t.Errorf("path = %q, want %q", r.URL.EscapedPath(), want)
					}
					if r.Method != "POST" {
						t.Errorf("method = %s", r.Method)
					}
					b, _ := io.ReadAll(r.Body)
					var got map[string]string
					if err := json.Unmarshal(b, &got); err != nil {
						t.Errorf("decoding %q: %v", b, err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if len(got) != 1 || got["reactionType"] != emoji {
						t.Errorf("body = %v, want reactionType %q", got, emoji)
					}
					w.WriteHeader(http.StatusNoContent)
				})
				if err := call(c, "19:abc@thread.v2", "1/2?3# 4", emoji); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestReactionErrorsPropagated(t *testing.T) {
	for status, want := range map[int]string{400: "BadRequest", 403: "permission denied", 404: "not found", 401: "unauthorized"} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"code":"BadRequest","message":"m"}}`)
			})
			for _, err := range []error{
				c.SetReaction(context.Background(), "c", "m", "👍"),
				c.UnsetReaction(context.Background(), "c", "m", "👍"),
			} {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want containing %q", err, want)
				}
			}
		})
	}
}

func TestReactionTokenErrorNoHTTP(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	tokErr := errors.New("no token")
	c.token = func(context.Context) (string, error) { return "", tokErr }
	if err := c.SetReaction(context.Background(), "c", "m", "👍"); err != tokErr {
		t.Errorf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Errorf("calls = %d", calls.Load())
	}
}
