package graph

import "context"

type Me struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var me Me
	if err := c.getJSON(ctx, "/me", &me); err != nil {
		return nil, err
	}
	return &me, nil
}
