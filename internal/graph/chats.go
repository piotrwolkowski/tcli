package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		var result chatsResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parsing chats response: %w", err)
		}

		allChats = append(allChats, result.Value...)

		if result.NextLink != "" {
			path = strings.TrimPrefix(result.NextLink, baseURL)
		} else {
			path = ""
		}
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
