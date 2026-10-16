package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSendMessageAttachments(t *testing.T) {
	att := Attachment{ID: "g1", ContentType: "reference", ContentURL: "https://x/f.txt", Name: "f.txt"}
	att2 := Attachment{ID: "g2", ContentType: "reference", ContentURL: "https://x/g.txt", Name: "g.txt"}
	tests := []struct {
		name        string
		text        string
		html        bool
		atts        []Attachment
		wantContent string
		wantType    string
		wantAtts    bool
	}{
		{"plain text escaped", "a < b & c\nnext", false, []Attachment{att},
			`a &lt; b &amp; c<br>next<attachment id="g1"></attachment>`, "html", true},
		{"empty text two attachments", "", false, []Attachment{att, att2},
			`<attachment id="g1"></attachment><attachment id="g2"></attachment>`, "html", true},
		{"html raw", "<b>x</b>", true, []Attachment{att},
			`<b>x</b><attachment id="g1"></attachment>`, "html", true},
		{"no attachments", "a < b", false, nil, "a < b", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw map[string]json.RawMessage
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					t.Fatalf("decode: %v", err)
				}
				writeJSON(t, w, map[string]any{"id": "m1"})
			})
			if _, err := c.SendMessage(context.Background(), "chat", tt.text, tt.html, tt.atts...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var body MessageBody
			json.Unmarshal(raw["body"], &body)
			if body.Content != tt.wantContent || body.ContentType != tt.wantType {
				t.Errorf("body = %+v, want content %q type %q", body, tt.wantContent, tt.wantType)
			}
			rawAtts, has := raw["attachments"]
			if has != tt.wantAtts {
				t.Fatalf("attachments key present = %v, want %v", has, tt.wantAtts)
			}
			if has {
				var got []Attachment
				json.Unmarshal(rawAtts, &got)
				if len(got) != len(tt.atts) {
					t.Fatalf("attachments = %+v", got)
				}
				for i := range got {
					if got[i] != tt.atts[i] {
						t.Errorf("attachment %d = %+v, want %+v", i, got[i], tt.atts[i])
					}
				}
			}
		})
	}
}

func TestMessageParsesReactions(t *testing.T) {
	data := `{"id":"m1","reactions":[{"reactionType":"custom","displayName":"partyparrot","createdDateTime":"2026-01-01T10:00:00Z","user":{"user":{"id":"u1"}}},{"reactionType":"👍","user":{"user":{"id":"u2"}}}]}`
	var m Message
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Reactions) != 2 {
		t.Fatalf("reactions = %+v", m.Reactions)
	}
	r := m.Reactions[0]
	if r.ReactionType != "custom" || r.DisplayName != "partyparrot" || r.User == nil || r.User.User == nil || r.User.User.ID != "u1" {
		t.Errorf("reaction 0 = %+v", r)
	}
	if m.Reactions[1].ReactionType != "👍" || m.Reactions[1].User.User.ID != "u2" {
		t.Errorf("reaction 1 = %+v", m.Reactions[1])
	}
}
