package graph

import (
	"io"
	"net/http"
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
			name:       "other error includes code and message",
			resp:       makeResp(404, `{"error":{"code":"ItemNotFound","message":"No chat with ID xyz."}}`),
			wantSubstr: "ItemNotFound",
		},
		{
			name:       "other error includes Graph message text",
			resp:       makeResp(404, `{"error":{"code":"ItemNotFound","message":"No chat with ID xyz."}}`),
			wantSubstr: "No chat with ID xyz",
		},
		{
			name:       "non-JSON body falls back to status code format",
			resp:       makeResp(500, `internal server error`),
			wantSubstr: "500",
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
		resp    *http.Response
		attempt int
		want    time.Duration
	}{
		{
			name:    "Retry-After header is used when present",
			resp:    makeRespWithHeader(429, "", "Retry-After", "30"),
			attempt: 0,
			want:    30 * time.Second,
		},
		{
			name:    "exponential backoff attempt 0",
			resp:    makeResp(429, ""),
			attempt: 0,
			want:    2 * time.Second,
		},
		{
			name:    "exponential backoff attempt 1",
			resp:    makeResp(429, ""),
			attempt: 1,
			want:    4 * time.Second,
		},
		{
			name:    "exponential backoff attempt 2",
			resp:    makeResp(429, ""),
			attempt: 2,
			want:    8 * time.Second,
		},
		{
			name:    "non-numeric Retry-After falls back to backoff",
			resp:    makeRespWithHeader(429, "", "Retry-After", "soon"),
			attempt: 0,
			want:    2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryAfter(tt.resp, tt.attempt)
			if got != tt.want {
				t.Errorf("retryAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}
