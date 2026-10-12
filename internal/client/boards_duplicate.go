package client

import (
	"context"
	"fmt"
	"net/url"
)

// DuplicateBoard copies the board sourceID under a new name and returns the
// copy's id.
//
// POST /api/boards/{id}/duplicate copies the whole board in one transaction:
// every board column (visibility and all appearance settings), every layout
// with its column and gutter counts, the sections, and the items with their
// options, positions and integration links. The copy belongs to the caller and,
// unlike CreateBoard, is never made the caller's home board.
//
// It needs the board-create permission, view access to the source, use access
// to every integration linked by the source's items, and admin when the source
// contains custom widgets. Homarr checks the name case-insensitively and
// answers 409 for a taken one. Present since at least Homarr 2.0.0.
func (c *Client) DuplicateBoard(ctx context.Context, sourceID, name string) (string, error) {
	body := struct {
		Name string `json:"name"`
	}{Name: name}
	var out struct {
		BoardID string `json:"boardId"`
	}
	if err := c.do(ctx, "POST", "/api/boards/"+url.PathEscape(sourceID)+"/duplicate", body, &out); err != nil {
		return "", err
	}
	if out.BoardID == "" {
		return "", fmt.Errorf("Homarr accepted the board copy but returned no boardId")
	}
	return out.BoardID, nil
}
