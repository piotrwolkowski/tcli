package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"
)

type Message struct {
	ID              string          `json:"id"`
	MessageType     string          `json:"messageType"`
	CreatedDateTime string          `json:"createdDateTime"`
	DeletedDateTime string          `json:"deletedDateTime"`
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

// isUserMessage reports whether m is a real, non-deleted chat message
// (as opposed to a system event or a deleted message).
func isUserMessage(m Message) bool {
	return m.MessageType == "message" && m.DeletedDateTime == ""
}

// ListMessagesAfterMine fetches messages from chatID, newest-first by
// createdDateTime, stopping once it sees a real message authored by meID.
// System and deleted messages are skipped. Returns messages newer than the
// caller's most recent message, in chronological order (oldest first).
// If the user has never posted in the chat, all fetched messages (up to
// maxPages worth) are returned.
func (c *Client) ListMessagesAfterMine(ctx context.Context, chatID, meID string, maxPages int) ([]Message, error) {
	var collected []Message
	// Explicit ordering: Graph defaults to lastModifiedDateTime desc, which
	// lets an edited old message of ours end the scan early. $top=50 is the max.
	path := fmt.Sprintf("/me/chats/%s/messages?$orderby=createdDateTime%%20desc&$top=50", url.PathEscape(chatID))

scan:
	for pages := 0; path != "" && pages < maxPages; pages++ {
		var result messagesResponse
		if err := c.getJSON(ctx, path, &result); err != nil {
			return nil, err
		}

		for _, m := range result.Value {
			if !isUserMessage(m) {
				continue
			}
			if m.From != nil && m.From.User != nil && m.From.User.ID == meID {
				break scan
			}
			collected = append(collected, m)
		}

		path = result.NextLink
	}

	sort.SliceStable(collected, func(i, j int) bool {
		return createdBefore(collected[i], collected[j])
	})
	return collected, nil
}

// createdBefore compares messages by parsed createdDateTime, falling back
// to string comparison when either timestamp fails to parse.
func createdBefore(a, b Message) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a.CreatedDateTime)
	tb, errB := time.Parse(time.RFC3339Nano, b.CreatedDateTime)
	if errA != nil || errB != nil {
		return a.CreatedDateTime < b.CreatedDateTime
	}
	return ta.Before(tb)
}

type SendMessageRequest struct {
	Body MessageBody `json:"body"`
}

type MessageBody struct {
	ContentType string `json:"contentType,omitempty"`
	Content     string `json:"content"`
}

type SendMessageResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdDateTime"`
}

// SendMessage posts content to chatID. With html set the body is sent as
// contentType "html"; otherwise Graph treats it as plain text, where Teams
// collapses newlines.
func (c *Client) SendMessage(ctx context.Context, chatID, content string, html bool) (*SendMessageResponse, error) {
	payload := SendMessageRequest{
		Body: MessageBody{Content: content},
	}
	if html {
		payload.Body.ContentType = "html"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling message: %w", err)
	}

	path := fmt.Sprintf("/me/chats/%s/messages", url.PathEscape(chatID))
	resp, err := c.do(ctx, "POST", path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	var result SendMessageResponse
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
