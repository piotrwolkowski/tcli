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
