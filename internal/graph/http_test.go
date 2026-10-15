package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// newTestClient returns a Client pointed at an httptest server running h.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Client{
		http:    srv.Client(),
		baseURL: srv.URL,
		token:   func(context.Context) (string, error) { return "test-token", nil },
	}, srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

func msg(id, from, created string) map[string]any {
	return map[string]any{
		"id":              id,
		"messageType":     "message",
		"createdDateTime": created,
		"from":            map[string]any{"user": map[string]any{"id": from}},
		"body":            map[string]any{"contentType": "text", "content": id},
	}
}

func sysMsg(id, created string) map[string]any {
	m := msg(id, "", created)
	m["messageType"] = "systemEventMessage"
	m["from"] = nil
	return m
}

func deletedMsg(id, from, created string) map[string]any {
	m := msg(id, from, created)
	m["deletedDateTime"] = created
	return m
}

func ids(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestListMessagesAfterMine(t *testing.T) {
	const me = "me"
	tests := []struct {
		name      string
		pages     [][]map[string]any // newest first, as Graph returns them
		maxPages  int
		want      []string
		wantCalls int
	}{
		{
			name: "stops at user's latest message",
			pages: [][]map[string]any{{
				msg("c", "bob", "2026-01-01T10:03:00Z"),
				msg("b", "alice", "2026-01-01T10:02:00Z"),
				msg("mine", me, "2026-01-01T10:01:00Z"),
				msg("old", "bob", "2026-01-01T10:00:00Z"),
			}},
			maxPages:  5,
			want:      []string{"b", "c"},
			wantCalls: 1,
		},
		{
			name: "skips system and deleted messages, deleted own message does not stop scan",
			pages: [][]map[string]any{{
				msg("c", "bob", "2026-01-01T10:05:00Z"),
				sysMsg("sys", "2026-01-01T10:04:00Z"),
				deletedMsg("delmine", me, "2026-01-01T10:03:00Z"),
				msg("b", "alice", "2026-01-01T10:02:00Z"),
				msg("mine", me, "2026-01-01T10:01:00Z"),
			}},
			maxPages:  5,
			want:      []string{"b", "c"},
			wantCalls: 1,
		},
		{
			name: "follows absolute nextLink across pages",
			pages: [][]map[string]any{
				{msg("d", "bob", "2026-01-01T10:04:00Z"), msg("c", "bob", "2026-01-01T10:03:00Z")},
				{msg("b", "alice", "2026-01-01T10:02:00Z"), msg("mine", me, "2026-01-01T10:01:00Z")},
				{msg("never", "bob", "2026-01-01T10:00:00Z")},
			},
			maxPages:  5,
			want:      []string{"b", "c", "d"},
			wantCalls: 2,
		},
		{
			name: "respects maxPages",
			pages: [][]map[string]any{
				{msg("d", "bob", "2026-01-01T10:04:00Z")},
				{msg("c", "bob", "2026-01-01T10:03:00Z")},
				{msg("b", "bob", "2026-01-01T10:02:00Z")},
			},
			maxPages:  2,
			want:      []string{"c", "d"},
			wantCalls: 2,
		},
		{
			name: "user never posted returns everything",
			pages: [][]map[string]any{
				{msg("b", "bob", "2026-01-01T10:02:00Z")},
				{msg("a", "alice", "2026-01-01T10:01:00Z")},
			},
			maxPages:  5,
			want:      []string{"a", "b"},
			wantCalls: 2,
		},
		{
			name: "chronological order by parsed time, not string compare",
			pages: [][]map[string]any{{
				// Different offsets: string order would be wrong.
				msg("later", "bob", "2026-01-01T12:00:00+02:00"),    // 10:00Z
				msg("earlier", "bob", "2026-01-01T09:30:00.5Z"),     // 09:30:00.5Z
				msg("earliest", "bob", "2026-01-01T11:00:00+03:00"), // 08:00Z
			}},
			maxPages:  5,
			want:      []string{"earliest", "earlier", "later"},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var srvURL string
			c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q", got)
				}
				page := 0
				if p := r.URL.Query().Get("page"); p != "" {
					fmt.Sscan(p, &page)
				} else {
					if r.URL.EscapedPath() != "/me/chats/19:abc@thread.v2/messages" {
						t.Errorf("path = %q", r.URL.EscapedPath())
					}
					q := r.URL.Query()
					if got := q.Get("$orderby"); got != "createdDateTime desc" {
						t.Errorf("$orderby = %q, want %q", got, "createdDateTime desc")
					}
					if got := q.Get("$top"); got != "50" {
						t.Errorf("$top = %q, want 50", got)
					}
				}
				resp := map[string]any{"value": tt.pages[page]}
				if page+1 < len(tt.pages) {
					resp["@odata.nextLink"] = fmt.Sprintf("%s/me/chats/x/messages?page=%d", srvURL, page+1)
				}
				writeJSON(t, w, resp)
			})
			srvURL = srv.URL

			got, err := c.ListMessagesAfterMine(context.Background(), "19:abc@thread.v2", me, tt.maxPages)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if g, w := strings.Join(ids(got), ","), strings.Join(tt.want, ","); g != w {
				t.Errorf("ids = %s, want %s", g, w)
			}
			if calls != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestListChatsPagination(t *testing.T) {
	var srvURL string
	var paths []string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("skiptoken") == "" {
			writeJSON(t, w, map[string]any{
				"value":           []map[string]any{{"id": "1", "topic": "One"}},
				"@odata.nextLink": srvURL + "/me/chats?$expand=members&$top=50&skiptoken=abc",
			})
			return
		}
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{{"id": "2", "topic": "Two"}},
		})
	})
	srvURL = srv.URL

	chats, err := c.ListChats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chats) != 2 || chats[0].ID != "1" || chats[1].ID != "2" {
		t.Errorf("chats = %+v, want IDs 1,2", chats)
	}
	if len(paths) != 2 {
		t.Fatalf("requests = %v, want 2", paths)
	}
	if !strings.Contains(paths[1], "skiptoken=abc") {
		t.Errorf("second request = %q, want nextLink followed", paths[1])
	}
}

func TestSendMessage(t *testing.T) {
	tests := []struct {
		name            string
		chatID          string
		html            bool
		wantPath        string
		wantContentType string
	}{
		{
			name:            "plain text omits contentType",
			chatID:          "19:abc@thread.v2",
			wantPath:        "/me/chats/19:abc@thread.v2/messages",
			wantContentType: "",
		},
		{
			name:            "html sets contentType",
			chatID:          "19:abc@thread.v2",
			html:            true,
			wantPath:        "/me/chats/19:abc@thread.v2/messages",
			wantContentType: "html",
		},
		{
			name:     "chat ID is path-escaped",
			chatID:   "a/b?c#d e",
			wantPath: "/me/chats/a%2Fb%3Fc%23d%20e/messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.EscapedPath() != tt.wantPath {
					t.Errorf("path = %q, want %q", r.URL.EscapedPath(), tt.wantPath)
				}
				var raw map[string]map[string]any
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					t.Fatalf("decoding body: %v", err)
				}
				ct, has := raw["body"]["contentType"]
				if tt.wantContentType == "" && has {
					t.Errorf("contentType = %v, want omitted", ct)
				}
				if tt.wantContentType != "" && ct != tt.wantContentType {
					t.Errorf("contentType = %v, want %q", ct, tt.wantContentType)
				}
				if raw["body"]["content"] != "hello" {
					t.Errorf("content = %v, want hello", raw["body"]["content"])
				}
				w.WriteHeader(http.StatusCreated)
				writeJSON(t, w, map[string]any{"id": "m1", "createdDateTime": "2026-01-01T10:00:00Z"})
			})

			resp, err := c.SendMessage(context.Background(), tt.chatID, "hello", tt.html)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.ID != "m1" {
				t.Errorf("ID = %q, want m1", resp.ID)
			}
		})
	}
}

func TestDoRetriesOn429WithBodyReplay(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(t, w, map[string]any{"id": "m1"})
	})

	resp, err := c.SendMessage(context.Background(), "chat", "payload", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "m1" {
		t.Errorf("ID = %q, want m1", resp.ID)
	}
	if len(bodies) != 3 {
		t.Fatalf("attempts = %d, want 3", len(bodies))
	}
	for i, b := range bodies {
		if !strings.Contains(b, `"content":"payload"`) {
			t.Errorf("attempt %d body = %q, want replayed payload", i, b)
		}
	}
}

func TestDoGivesUpAfterMaxRetries(t *testing.T) {
	var calls int
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.Me(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v, want rate limited", err)
	}
	if calls != maxRetries+1 {
		t.Errorf("calls = %d, want %d", calls, maxRetries+1)
	}
}
