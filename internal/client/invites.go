package client

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// InviteCreator is the embedded creator object in an invite.
type InviteCreator struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

// Invite is the shape returned by GET /api/invites. The token is deliberately
// excluded from that response and is only ever returned once, at creation.
type Invite struct {
	ID             string        `json:"id"`
	ExpirationDate string        `json:"expirationDate"`
	Creator        InviteCreator `json:"creator"`
}

// ListInvites returns every invite. Requires an admin API key.
func (c *Client) ListInvites(ctx context.Context) ([]Invite, error) {
	var invites []Invite
	if err := c.do(ctx, "GET", "/api/invites", nil, &invites); err != nil {
		return nil, err
	}
	return invites, nil
}

// GetInvite finds an invite by id, with a synthetic 404 when absent. Homarr has
// no per-invite GET, so this filters the list.
func (c *Client) GetInvite(ctx context.Context, id string) (*Invite, error) {
	invites, err := c.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	for i := range invites {
		if invites[i].ID == id {
			return &invites[i], nil
		}
	}
	return nil, &Error{
		StatusCode: 404,
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("invite %q is not present in GET /api/invites", id),
	}
}

// CreateInvite creates an invite and returns its id and single-use token.
func (c *Client) CreateInvite(ctx context.Context, expiration time.Time) (id, token string, err error) {
	body := struct {
		ExpirationDate string `json:"expirationDate"`
	}{ExpirationDate: expiration.UTC().Format(time.RFC3339Nano)}

	var out struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := c.do(ctx, "POST", "/api/invites", body, &out); err != nil {
		return "", "", err
	}
	if out.ID == "" {
		return "", "", fmt.Errorf("Homarr accepted the invite but returned no id")
	}
	return out.ID, out.Token, nil
}

// DeleteInvite removes an invite. Unlike most Homarr deletes this one 404s when
// the invite is already gone.
func (c *Client) DeleteInvite(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/invites/"+url.PathEscape(id), nil, nil)
}
