package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

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
