package client

import (
	"context"
	"fmt"
	"net/url"
)

// App mirrors selectAppSchema: the shape returned by GET /api/apps and
// GET /api/apps/{id}.
type App struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IconURL     string  `json:"iconUrl"`
	Href        *string `json:"href"`
	PingURL     *string `json:"pingUrl"`
}

// AppRequest is the body for creating and updating an app.
//
// Every field is required by appManageSchema even though several are nullable,
// so all of them are always serialised — including explicit nulls. This is why
// PATCH /api/apps/{id} behaves as a full replace despite the HTTP verb.
type AppRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IconURL     string  `json:"iconUrl"`
	Href        *string `json:"href"`
	PingURL     *string `json:"pingUrl"`
}

// ListApps returns every app visible to the API key.
//
// Homarr 2.0.0 narrowed GET /api/apps to the app-modify-all permission, because
// the full catalogue exposes internal URLs. A key without it now gets a 403
// where it used to get a list, so this falls back to GET /api/apps/selectable,
// which is open to any authenticated caller and returns the same six fields.
// The fallback is best-effort: if it fails too, the original 403 is reported,
// since that is the error worth acting on.
func (c *Client) ListApps(ctx context.Context) ([]App, error) {
	var apps []App
	err := c.do(ctx, "GET", "/api/apps", nil, &apps)
	if err == nil {
		return apps, nil
	}
	if !IsForbidden(err) {
		return nil, err
	}

	var selectable []App
	if fallbackErr := c.do(ctx, "GET", "/api/apps/selectable", nil, &selectable); fallbackErr != nil {
		return nil, err
	}
	return selectable, nil
}

// GetApp fetches a single app. A missing app yields a 404, detectable with
// IsNotFound.
func (c *Client) GetApp(ctx context.Context, id string) (*App, error) {
	var app App
	if err := c.do(ctx, "GET", "/api/apps/"+url.PathEscape(id), nil, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

// CreateApp creates an app and returns it as stored.
func (c *Client) CreateApp(ctx context.Context, req AppRequest) (*App, error) {
	// The create response carries both `appId` and `id` for backwards
	// compatibility; they hold the same value and we read `id`.
	var app App
	if err := c.do(ctx, "POST", "/api/apps", req, &app); err != nil {
		return nil, err
	}
	if app.ID == "" {
		return nil, fmt.Errorf("Homarr accepted the app but returned no id")
	}
	return &app, nil
}

// UpdateApp replaces every mutable field of an app.
func (c *Client) UpdateApp(ctx context.Context, id string, req AppRequest) error {
	return c.do(ctx, "PATCH", "/api/apps/"+url.PathEscape(id), req, nil)
}

// DeleteApp removes an app. Homarr answers 200 even when the app is already
// gone, so this is idempotent.
func (c *Client) DeleteApp(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/apps/"+url.PathEscape(id), nil, nil)
}
