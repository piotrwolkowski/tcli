package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// SetReaction adds reaction (a unicode emoji) to a chat message as the
// signed-in user. Requires ChatMessage.Send.
func (c *Client) SetReaction(ctx context.Context, chatID, messageID, reaction string) error {
	return c.postReaction(ctx, "setReaction", chatID, messageID, reaction)
}

// UnsetReaction removes the signed-in user's reaction from a chat message.
func (c *Client) UnsetReaction(ctx context.Context, chatID, messageID, reaction string) error {
	return c.postReaction(ctx, "unsetReaction", chatID, messageID, reaction)
}

func (c *Client) postReaction(ctx context.Context, action, chatID, messageID, reaction string) error {
	data, err := json.Marshal(map[string]string{"reactionType": reaction})
	if err != nil {
		return fmt.Errorf("marshalling reaction: %w", err)
	}

	// Only the /chats/{id} form of these actions is documented, not /me/chats.
	path := fmt.Sprintf("/chats/%s/messages/%s/%s", url.PathEscape(chatID), url.PathEscape(messageID), action)
	resp, err := c.do(ctx, "POST", path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
