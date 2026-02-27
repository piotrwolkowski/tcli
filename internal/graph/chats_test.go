package graph

import "testing"

func TestChatDisplayName(t *testing.T) {
	tests := []struct {
		name string
		chat Chat
		want string
	}{
		{
			name: "topic takes precedence over members",
			chat: Chat{
				Topic:   "Project Alpha",
				Members: []ChatMember{{DisplayName: "Alice"}, {DisplayName: "Bob"}},
			},
			want: "Project Alpha",
		},
		{
			name: "members joined when no topic",
			chat: Chat{
				Members: []ChatMember{{DisplayName: "Alice"}, {DisplayName: "Bob"}},
			},
			want: "Alice, Bob",
		},
		{
			name: "single member",
			chat: Chat{
				Members: []ChatMember{{DisplayName: "Alice"}},
			},
			want: "Alice",
		},
		{
			name: "members with empty display names are skipped",
			chat: Chat{
				Members: []ChatMember{{DisplayName: ""}, {DisplayName: "Bob"}},
			},
			want: "Bob",
		},
		{
			name: "no topic and no members",
			chat: Chat{},
			want: "(unnamed)",
		},
		{
			name: "all members have empty display names",
			chat: Chat{
				Members: []ChatMember{{DisplayName: ""}, {DisplayName: ""}},
			},
			want: "(unnamed)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ChatDisplayName(tt.chat)
			if got != tt.want {
				t.Errorf("ChatDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}
