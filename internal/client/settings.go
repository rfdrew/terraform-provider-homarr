package client

import "context"

// ServerBoardSettings is the instance-wide board defaults object exposed by
// GET and PATCH /api/settings/board.
type ServerBoardSettings struct {
	HomeBoardID           *string `json:"homeBoardId"`
	MobileHomeBoardID     *string `json:"mobileHomeBoardId"`
	EnableStatusByDefault bool    `json:"enableStatusByDefault"`
	ForceDisableStatus    bool    `json:"forceDisableStatus"`
}

// ServerBoardSettingsPatch is a partial update to the instance-wide board
// defaults. Nil fields are omitted and left untouched.
type ServerBoardSettingsPatch struct {
	HomeBoardID           *string `json:"homeBoardId,omitempty"`
	MobileHomeBoardID     *string `json:"mobileHomeBoardId,omitempty"`
	EnableStatusByDefault *bool   `json:"enableStatusByDefault,omitempty"`
	ForceDisableStatus    *bool   `json:"forceDisableStatus,omitempty"`
}

// GetServerBoardSettings reads the instance-wide board defaults. Requires an
// admin API key.
func (c *Client) GetServerBoardSettings(ctx context.Context) (*ServerBoardSettings, error) {
	var settings ServerBoardSettings
	if err := c.do(ctx, "GET", "/api/settings/board", nil, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateServerBoardSettings applies a partial patch and returns the settings as
// stored afterwards.
func (c *Client) UpdateServerBoardSettings(ctx context.Context, patch ServerBoardSettingsPatch) (*ServerBoardSettings, error) {
	var settings ServerBoardSettings
	if err := c.do(ctx, "PATCH", "/api/settings/board", patch, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// Info is the response of GET /api/info.
type Info struct {
	Version string `json:"version"`
}

// GetInfo reads the Homarr version. It is the cheapest authenticated call and
// doubles as the provider's connectivity check.
func (c *Client) GetInfo(ctx context.Context) (*Info, error) {
	var info Info
	if err := c.do(ctx, "GET", "/api/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
