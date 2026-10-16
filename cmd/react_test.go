package cmd

import "testing"

func TestReactionEmoji(t *testing.T) {
	tests := map[string]string{
		"like":    "👍",
		"HEART":   "❤️",
		" laugh ": "😆",
		"👍":       "👍",
		"🎉":       "🎉",
	}
	for in, want := range tests {
		if got := reactionEmoji(in); got != want {
			t.Errorf("reactionEmoji(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReactionEmojiEdgeCases(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"whitespace only", " \t\n ", ""},
		{"mixed case", "LiKe", "👍"},
		{"all names", "surprised", "😮"},
		{"unknown word passes through", "thumbsup", "thumbsup"},
		{"unknown word is trimmed", "  thumbsup ", "thumbsup"},
		{"name with inner space is unknown", "li ke", "li ke"},
		{"already an emoji with spaces", " 👍 ", "👍"},
		{"multi-codepoint emoji untouched", "👍🏽", "👍🏽"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reactionEmoji(tt.in); got != tt.want {
				t.Errorf("reactionEmoji(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
