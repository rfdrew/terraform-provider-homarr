package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// BoardItemSize is the optional grid size of a new board item.
type BoardItemSize struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}

// BoardItemCreateRequest is the body for POST /api/boards/items.
//
// Options is the widget's free-form option object; Homarr stores it as given
// and validates nothing in it beyond a few widget kinds. It must be a JSON
// object, so AddBoardItem sends {} in its place when it is empty.
type BoardItemCreateRequest struct {
	BoardID        string          `json:"boardId"`
	Kind           string          `json:"kind"`
	Options        json.RawMessage `json:"options"`
	Size           *BoardItemSize  `json:"size,omitempty"`
	IntegrationIDs []string        `json:"integrationIds"`
}

// AddBoardItem places a widget or app tile on a board and returns its id.
//
// Homarr puts the item at the first free cell of the board's main section in
// every layout, Mobile included. Nothing over REST reads an item back or
// deletes a single one, so the id is all a caller ever learns about it.
//
// It needs modify access to the board (404 without it, as for a missing
// board), use access to every listed integration (404 "Integration not found"
// otherwise) and admin for custom widgets. Present since at least Homarr 2.0.0.
func (c *Client) AddBoardItem(ctx context.Context, req BoardItemCreateRequest) (string, error) {
	if len(req.Options) == 0 {
		req.Options = json.RawMessage("{}")
	}
	if req.IntegrationIDs == nil {
		req.IntegrationIDs = []string{}
	}
	var out struct {
		ItemID string `json:"itemId"`
	}
	if err := c.do(ctx, "POST", "/api/boards/items", req, &out); err != nil {
		return "", err
	}
	if out.ItemID == "" {
		return "", fmt.Errorf("Homarr accepted the board item but returned no itemId")
	}
	return out.ItemID, nil
}

// BoardItemLayout is the body for
// PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}: an item's
// position and size in one layout, in grid cells, relative to the item's
// section. All four are required.
type BoardItemLayout struct {
	XOffset int64 `json:"xOffset"`
	YOffset int64 `json:"yOffset"`
	Width   int64 `json:"width"`
	Height  int64 `json:"height"`
}

// MoveBoardItem moves or resizes an item in one of its board's layouts,
// leaving the other layouts alone.
//
// The item stays in its section. A placement that does not fit the section's
// columns is refused with 400 "Item layout exceeds its section bounds", one
// that overlaps another item or section with 409, and a layout or item that is
// not on the board with 404 "Item layout not found on this board". Like
// AddBoardItem it needs modify access to the board, and answers 404 "Board not
// found" without it.
//
// The only layout id a REST client can learn is the Base layout's, from
// CreateBoard. Present since at least Homarr 2.0.0.
func (c *Client) MoveBoardItem(ctx context.Context, boardID, itemID, layoutID string, layout BoardItemLayout) error {
	path := "/api/boards/" + url.PathEscape(boardID) + "/items/" + url.PathEscape(itemID) +
		"/layouts/" + url.PathEscape(layoutID)
	return c.do(ctx, "PATCH", path, layout, nil)
}
