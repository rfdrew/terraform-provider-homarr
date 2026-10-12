package client

import (
	"context"
	"regexp"
	"strconv"
)

// ServerBoardSettings is the instance-wide board defaults object exposed by
// GET and PATCH /api/settings/board. Both verbs always report every field; an
// unset home board is null.
type ServerBoardSettings struct {
	HomeBoardID           *string `json:"homeBoardId"`
	MobileHomeBoardID     *string `json:"mobileHomeBoardId"`
	EnableStatusByDefault bool    `json:"enableStatusByDefault"`
	ForceDisableStatus    bool    `json:"forceDisableStatus"`
}

// ServerBoardSettingsPatch is a partial update to the instance-wide board
// defaults, keyed by Homarr's field names: homeBoardId, mobileHomeBoardId,
// enableStatusByDefault and forceDisableStatus.
//
// It is a map rather than a struct for the same reason as UserPreferencesPatch:
// the two home board ids are nullable, and Homarr reads an explicit null as
// "clear it" but an omitted key as "leave it alone". A struct with `omitempty`
// cannot send that null, and one without it would clear every board the caller
// did not mention.
type ServerBoardSettingsPatch map[string]any

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
// stored afterwards. Unlike the preferences endpoint, Homarr accepts an empty
// patch here and simply answers with the current settings.
func (c *Client) UpdateServerBoardSettings(ctx context.Context, patch ServerBoardSettingsPatch) (*ServerBoardSettings, error) {
	// A nil map would encode as a bare null, which Homarr's object schema
	// rejects; an empty patch has to go out as {}.
	if patch == nil {
		patch = ServerBoardSettingsPatch{}
	}
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

// releasePattern matches the major.minor.patch release a Homarr version starts
// with; a pre-release or build suffix after it is ignored.
var releasePattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// AtLeast reports whether this Homarr is release major.minor.patch or newer.
//
// known is false when the version does not start with a major.minor.patch
// release, as a custom build may report; atLeast is meaningless then. Homarr
// answers 404 for a route a release lacks, so callers use this to tell an older
// Homarr from a missing object rather than to refuse a call up front.
func (i *Info) AtLeast(major, minor, patch int) (atLeast, known bool) {
	m := releasePattern.FindStringSubmatch(i.Version)
	if m == nil {
		return false, false
	}
	want := [3]int{major, minor, patch}
	for k := range want {
		got, err := strconv.Atoi(m[k+1])
		if err != nil {
			return false, false
		}
		if got != want[k] {
			return got > want[k], true
		}
	}
	return true, true
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
