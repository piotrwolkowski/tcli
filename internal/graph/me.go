package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type Me struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func (c *Client) Me(ctx context.Context) (*Me, error) {
	resp, err := c.do(ctx, "GET", "/me", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var me Me
	if err := json.Unmarshal(body, &me); err != nil {
		return nil, fmt.Errorf("parsing /me response: %w", err)
	}
	return &me, nil
}
