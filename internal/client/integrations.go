package client

import (
	"context"
	"net/url"
)

// IntegrationConnection is the outcome of GET
// /api/integrations/{id}/test-connection. Error is null on success, and
// otherwise one of missingSecret, authorization, certificate, request,
// statusCode, parse or unknown: Homarr reports only the category, never the
// upstream message.
type IntegrationConnection struct {
	Success bool    `json:"success"`
	Error   *string `json:"error"`
}

// TestIntegrationConnection runs Homarr's connection check for a saved
// integration with its stored credentials. Every call contacts the integrated
// service, and may log in to it; nothing in Homarr is changed.
//
// Requires full access to the integration: integration-full-all, which admins
// have, or a "full" permission on that integration. Homarr answers 404, not
// 403, when the key lacks it, exactly as for an integration that does not
// exist. Requires Homarr 2.1.0 or newer; older releases have no such route and
// answer 404 as well.
func (c *Client) TestIntegrationConnection(ctx context.Context, id string) (*IntegrationConnection, error) {
	var result IntegrationConnection
	if err := c.do(ctx, "GET", "/api/integrations/"+url.PathEscape(id)+"/test-connection", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
