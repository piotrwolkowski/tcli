package graph

import (
	"context"
	"strings"
)

type Chat struct {
	ID       string       `json:"id"`
	Topic    string       `json:"topic"`
	ChatType string       `json:"chatType"`
	Members  []ChatMember `json:"members"`
}

type ChatMember struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type chatsResponse struct {
	Value    []Chat `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

func (c *Client) ListChats(ctx context.Context) ([]Chat, error) {
	var allChats []Chat
	path := "/me/chats?$expand=members&$top=50"

	for path != "" {
		var result chatsResponse
		if err := c.getJSON(ctx, path, &result); err != nil {
			return nil, err
		}
		allChats = append(allChats, result.Value...)
		// nextLink is an absolute URL; do() uses it as-is.
		path = result.NextLink
	}

	return allChats, nil
}

func ChatDisplayName(chat Chat) string {
	if chat.Topic != "" {
		return chat.Topic
	}
	var names []string
	for _, m := range chat.Members {
		if m.DisplayName != "" {
			names = append(names, m.DisplayName)
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	return "(unnamed)"
}
