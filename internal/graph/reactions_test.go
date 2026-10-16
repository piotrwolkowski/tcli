package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSetUnsetReaction(t *testing.T) {
	tests := []struct {
		name   string
		action string
		call   func(c *Client) error
	}{
		{"set", "setReaction", func(c *Client) error {
			return c.SetReaction(context.Background(), "a/b?c", "m/1", "👍")
		}},
		{"unset", "unsetReaction", func(c *Client) error {
			return c.UnsetReaction(context.Background(), "a/b?c", "m/1", "👍")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			var body map[string]string
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("method = %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
				}
				gotPath = r.URL.EscapedPath()
				json.NewDecoder(r.Body).Decode(&body)
				w.WriteHeader(http.StatusNoContent)
			})
			if err := tt.call(c); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if want := "/chats/a%2Fb%3Fc/messages/m%2F1/" + tt.action; gotPath != want {
				t.Errorf("path = %q, want %q", gotPath, want)
			}
			if body["reactionType"] != "👍" || len(body) != 1 {
				t.Errorf("body = %v", body)
			}
		})
		t.Run(tt.name+" not found", func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})
			err := tt.call(c)
			if err == nil || !strings.Contains(err.Error(), "not found") {
				t.Errorf("err = %v, want not found", err)
			}
		})
	}
}
