package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Message struct {
	ID              string          `json:"id"`
	CreatedDateTime string          `json:"createdDateTime"`
	From            *MessageFrom    `json:"from"`
	Body            MessageBodyFull `json:"body"`
}

type MessageFrom struct {
	User *MessageUser `json:"user"`
}

type MessageUser struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type MessageBodyFull struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type messagesResponse struct {
	Value    []Message `json:"value"`
	NextLink string    `json:"@odata.nextLink"`
}

// ListMessagesAfterMine fetches messages from chatID, newest-first, stopping
// once it sees a message authored by meID. Returns messages newer than the
// caller's most recent message, in chronological order (oldest first).
// If the user has never posted in the chat, all fetched messages (up to
// maxPages worth) are returned.
func (c *Client) ListMessagesAfterMine(ctx context.Context, chatID, meID string, maxPages int) ([]Message, error) {
	var collected []Message
	foundMine := false
	path := fmt.Sprintf("/me/chats/%s/messages?$top=50", chatID)

	for pages := 0; path != "" && pages < maxPages; pages++ {
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}

		var result messagesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("parsing messages response: %w", err)
		}

		for _, m := range result.Value {
			if m.From != nil && m.From.User != nil && m.From.User.ID == meID {
				foundMine = true
				break
			}
			collected = append(collected, m)
		}

		if foundMine || result.NextLink == "" {
			break
		}
		path = strings.TrimPrefix(result.NextLink, baseURL)
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].CreatedDateTime < collected[j].CreatedDateTime
	})
	return collected, nil
}

type SendMessageRequest struct {
	Body MessageBody `json:"body"`
}

type MessageBody struct {
	Content string `json:"content"`
}

type SendMessageResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdDateTime"`
}

func (c *Client) SendMessage(ctx context.Context, chatID, content string) (*SendMessageResponse, error) {
	payload := SendMessageRequest{
		Body: MessageBody{Content: content},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling message: %w", err)
	}

	path := fmt.Sprintf("/me/chats/%s/messages", chatID)
	resp, err := c.do(ctx, "POST", path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result SendMessageResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &result, nil
}
