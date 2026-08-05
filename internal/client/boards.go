package client

import (
	"context"
	"fmt"
	"net/url"
)

// BoardCreator is the embedded creator object in a board summary.
type BoardCreator struct {
	ID    string  `json:"id"`
	Name  *string `json:"name"`
	Image *string `json:"image"`
	Email *string `json:"email"`
}

// BoardSummary mirrors boardSummarySchema, the only board shape the REST
// surface exposes (GET /api/boards).
//
// Note what is absent: the column count, and every field managed by
// PATCH /api/boards/{id}/settings apart from logoImageUrl. Those cannot be read
// back over REST at all.
type BoardSummary struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	LogoImageURL *string       `json:"logoImageUrl"`
	IsPublic     bool          `json:"isPublic"`
	Creator      *BoardCreator `json:"creator"`
	IsHome       bool          `json:"isHome"`
	IsMobileHome bool          `json:"isMobileHome"`
}

// BoardCreateRequest is the body for POST /api/boards.
type BoardCreateRequest struct {
	Name        string `json:"name"`
	ColumnCount int64  `json:"columnCount"`
	IsPublic    bool   `json:"isPublic"`
}

// BoardSettings is the body for PATCH /api/boards/{id}/settings.
//
// Every field is optional (the schema is .partial()), so all are pointers and
// omitted when nil. There is deliberately no matching read method: Homarr has
// no GET for per-board settings, only the tRPC-only full board query.
type BoardSettings struct {
	PageTitle                 *string  `json:"pageTitle,omitempty"`
	MetaTitle                 *string  `json:"metaTitle,omitempty"`
	LogoImageURL              *string  `json:"logoImageUrl,omitempty"`
	FaviconImageURL           *string  `json:"faviconImageUrl,omitempty"`
	BackgroundImageURL        *string  `json:"backgroundImageUrl,omitempty"`
	BackgroundImageAttachment *string  `json:"backgroundImageAttachment,omitempty"`
	BackgroundImageRepeat     *string  `json:"backgroundImageRepeat,omitempty"`
	BackgroundImageSize       *string  `json:"backgroundImageSize,omitempty"`
	PrimaryColor              *string  `json:"primaryColor,omitempty"`
	SecondaryColor            *string  `json:"secondaryColor,omitempty"`
	Opacity                   *float64 `json:"opacity,omitempty"`
	IconColor                 *string  `json:"iconColor,omitempty"`
	ItemRadius                *string  `json:"itemRadius,omitempty"`
	CustomCSS                 *string  `json:"customCss,omitempty"`
	DisableStatus             *bool    `json:"disableStatus,omitempty"`
}

// ListBoards returns every board visible to the API key.
func (c *Client) ListBoards(ctx context.Context) ([]BoardSummary, error) {
	var boards []BoardSummary
	if err := c.do(ctx, "GET", "/api/boards", nil, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

// GetBoard finds a board by id.
//
// Homarr has no GET /api/boards/{id}, so this filters the list. A board that is
// absent yields a synthetic 404 so callers can use IsNotFound uniformly.
func (c *Client) GetBoard(ctx context.Context, id string) (*BoardSummary, error) {
	boards, err := c.ListBoards(ctx)
	if err != nil {
		return nil, err
	}
	for i := range boards {
		if boards[i].ID == id {
			return &boards[i], nil
		}
	}
	return nil, &Error{
		StatusCode: 404,
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("board %q is not present in GET /api/boards", id),
	}
}

// GetBoardByName finds a board by its unique name, with the same 404 semantics
// as GetBoard.
func (c *Client) GetBoardByName(ctx context.Context, name string) (*BoardSummary, error) {
	boards, err := c.ListBoards(ctx)
	if err != nil {
		return nil, err
	}
	for i := range boards {
		if boards[i].Name == name {
			return &boards[i], nil
		}
	}
	return nil, &Error{
		StatusCode: 404,
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("board named %q is not present in GET /api/boards", name),
	}
}

// CreateBoard creates a board and returns its id.
func (c *Client) CreateBoard(ctx context.Context, req BoardCreateRequest) (string, error) {
	var out struct {
		BoardID string `json:"boardId"`
	}
	if err := c.do(ctx, "POST", "/api/boards", req, &out); err != nil {
		return "", err
	}
	if out.BoardID == "" {
		return "", fmt.Errorf("Homarr accepted the board but returned no boardId")
	}
	return out.BoardID, nil
}

// RenameBoard changes a board's name. Names must match ^[A-Za-z0-9-_]*$ and be
// unique; violations come back as 400 and 409 respectively.
func (c *Client) RenameBoard(ctx context.Context, id, name string) error {
	body := struct {
		Name string `json:"name"`
	}{Name: name}
	return c.do(ctx, "PATCH", "/api/boards/"+url.PathEscape(id)+"/name", body, nil)
}

// SetBoardVisibility flips a board between public and private. Homarr refuses
// to make a home board private.
func (c *Client) SetBoardVisibility(ctx context.Context, id string, isPublic bool) error {
	visibility := "private"
	if isPublic {
		visibility = "public"
	}
	body := struct {
		Visibility string `json:"visibility"`
	}{Visibility: visibility}
	return c.do(ctx, "PATCH", "/api/boards/"+url.PathEscape(id)+"/visibility", body, nil)
}

// UpdateBoardSettings applies a partial settings patch to a board.
func (c *Client) UpdateBoardSettings(ctx context.Context, id string, settings BoardSettings) error {
	return c.do(ctx, "PATCH", "/api/boards/"+url.PathEscape(id)+"/settings", settings, nil)
}

// DeleteBoard removes a board and everything on it.
func (c *Client) DeleteBoard(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/boards/"+url.PathEscape(id), nil, nil)
}
