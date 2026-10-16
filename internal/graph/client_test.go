package graph

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func makeResp(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func makeRespWithHeader(statusCode int, body, headerKey, headerVal string) *http.Response {
	resp := makeResp(statusCode, body)
	resp.Header.Set(headerKey, headerVal)
	return resp
}

func TestParseGraphError(t *testing.T) {
	tests := []struct {
		name       string
		resp       *http.Response
		wantSubstr string
	}{
		{
			name:       "401 returns login hint",
			resp:       makeResp(401, `{"error":{"code":"InvalidAuthenticationToken","message":"Access token has expired."}}`),
			wantSubstr: "tcli login",
		},
		{
			name:       "403 returns permission hint",
			resp:       makeResp(403, `{"error":{"code":"Forbidden","message":"Insufficient privileges."}}`),
			wantSubstr: "Azure app registration",
		},
		{
			name:       "401 without Graph body still returns login hint",
			resp:       makeResp(401, ``),
			wantSubstr: "tcli login",
		},
		{
			name:       "403 includes Graph code and message",
			resp:       makeResp(403, `{"error":{"code":"Forbidden","message":"Insufficient privileges."}}`),
			wantSubstr: "permission denied (Forbidden: Insufficient privileges.)",
		},
		{
			name:       "403 without Graph body still returns permission hint",
			resp:       makeResp(403, `nope`),
			wantSubstr: "Chat.Read, ChatMessage.Send, Files.Read.All and Files.ReadWrite",
		},
		{
			name:       "404 returns not found hint",
			resp:       makeResp(404, `{"error":{"code":"ItemNotFound","message":"No chat with ID xyz."}}`),
			wantSubstr: "not found (ItemNotFound: No chat with ID xyz.) — check the chat ID or alias",
		},
		{
			name:       "other error includes code and message",
			resp:       makeResp(400, `{"error":{"code":"BadRequest","message":"Invalid filter clause."}}`),
			wantSubstr: "BadRequest",
		},
		{
			name:       "other error includes Graph message text",
			resp:       makeResp(400, `{"error":{"code":"BadRequest","message":"Invalid filter clause."}}`),
			wantSubstr: "Invalid filter clause",
		},
		{
			name:       "non-JSON body falls back to status code format",
			resp:       makeResp(500, `internal server error`),
			wantSubstr: "500",
		},
		{
			name:       "503 says unavailable",
			resp:       makeResp(503, ``),
			wantSubstr: "Graph API unavailable (HTTP 503) — try again later",
		},
		{
			name:       "504 warns the request may have gone through",
			resp:       makeResp(504, ``),
			wantSubstr: "check the chat before retrying",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseGraphError(tt.resp)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("parseGraphError() = %q, want substring %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		header  string // Retry-After value; empty means the header is absent
		attempt int
		want    time.Duration
	}{
		{"Retry-After header is used when present", "30", 0, 30 * time.Second},
		{"header takes precedence over the attempt number", "30", 2, 30 * time.Second},
		{"zero", "0", 0, 0},
		{"zero ignores attempt", "0", 2, 0},
		{"negative counts as zero", "-5", 0, 0},
		{"59 seconds", "59", 0, 59 * time.Second},
		{"60 seconds equals maxRetryWait", "60", 0, maxRetryWait},
		{"61 seconds is not clamped", "61", 0, 61 * time.Second},
		{"a day is not clamped", "86400", 0, 24 * time.Hour},
		{"exactly maxRetryAfterSecs", strconv.Itoa(maxRetryAfterSecs), 0, maxRetryAfterSecs * time.Second},
		{"one past maxRetryAfterSecs is capped", strconv.FormatInt(maxRetryAfterSecs+1, 10), 0, maxRetryAfterSecs * time.Second},
		{"beyond int64 falls back to backoff", "99999999999999999999", 0, 2 * time.Second},
		{"exponential backoff attempt 0", "", 0, 2 * time.Second},
		{"exponential backoff attempt 1", "", 1, 4 * time.Second},
		{"exponential backoff attempt 2", "", 2, 8 * time.Second},
		{"non-numeric falls back to backoff", "soon", 0, 2 * time.Second},
		{"HTTP-date falls back to backoff", "Wed, 21 Oct 2026 07:28:00 GMT", 1, 4 * time.Second},
		{"float falls back to backoff", "1.5", 0, 2 * time.Second},
		{"whitespace-padded falls back to backoff", " 5 ", 0, 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := makeResp(429, "")
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}
			got := retryAfter(resp, tt.attempt)
			if got != tt.want {
				t.Errorf("retryAfter(%q, %d) = %v, want %v", tt.header, tt.attempt, got, tt.want)
			}
		})
	}
}

// A huge Retry-After must not overflow time.Duration into a negative
// (immediate) wait; it is capped at maxRetryAfterSecs, which still exceeds
// maxRetryWait so send fails fast instead of retrying.
func TestRetryAfterHugeValueCapped(t *testing.T) {
	resp := makeRespWithHeader(429, "", "Retry-After", "10000000000") // 1e10 s
	got := retryAfter(resp, 0)
	if want := maxRetryAfterSecs * time.Second; got != want {
		t.Errorf("retryAfter(1e10) = %v, want %v", got, want)
	}
	if got <= 0 {
		t.Errorf("retryAfter(1e10) = %v, want positive (no overflow)", got)
	}
	if got <= maxRetryWait {
		t.Errorf("retryAfter(1e10) = %v, want more than maxRetryWait so send fails fast", got)
	}
}
