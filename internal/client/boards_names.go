package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Board names are compared three different ways inside Homarr, and the
// helpers here follow whichever one applies:
//
//   - rename and duplicate refuse a name that matches any board's
//     case-insensitively (noBoardWithSimilarNameAsync, HTTP 409);
//   - create checks nothing and leaves it to the database's unique constraint,
//     which is case-sensitive on both SQLite and PostgreSQL, so "Infra" is
//     accepted next to "infra" and an exact duplicate fails with HTTP 500;
//   - board URLs are resolved case-insensitively, so of two names differing
//     only in case one board becomes unreachable in the UI.

// AmbiguousBoardNameError is returned by FindBoardByName when no board has the
// exact name but several match it case-insensitively.
type AmbiguousBoardNameError struct {
	Name    string
	Matches []BoardSummary
}

func (e *AmbiguousBoardNameError) Error() string {
	found := make([]string, 0, len(e.Matches))
	for _, board := range e.Matches {
		found = append(found, fmt.Sprintf("%q (id %s)", board.Name, board.ID))
	}
	return fmt.Sprintf("no board is named exactly %q, and %d boards match it case-insensitively: %s; "+
		"use the exact name or the board id", e.Name, len(e.Matches), strings.Join(found, ", "))
}

// FindBoardByName picks the board called name out of a GET /api/boards list.
//
// An exact match wins. Otherwise the name is matched case-insensitively, the
// way Homarr resolves board URLs; because REST-created boards can differ only
// in case, more than one such match is an AmbiguousBoardNameError rather than
// a guess. No match at all is a synthetic 404, as in GetBoard.
func FindBoardByName(boards []BoardSummary, name string) (*BoardSummary, error) {
	for i := range boards {
		if boards[i].Name == name {
			return &boards[i], nil
		}
	}
	matches := BoardsWithSimilarName(boards, name)
	switch len(matches) {
	case 0:
		return nil, &Error{
			StatusCode: http.StatusNotFound,
			Code:       "NOT_FOUND",
			Message:    fmt.Sprintf("board named %q is not present in GET /api/boards", name),
		}
	case 1:
		return &matches[0], nil
	default:
		return nil, &AmbiguousBoardNameError{Name: name, Matches: matches}
	}
}

// BoardsWithSimilarName returns the boards whose name equals name ignoring
// case, skipping the ids in ignoreIDs — the comparison Homarr makes before a
// rename or duplicate. Board names are ASCII-only, so strings.EqualFold agrees
// with the JavaScript toLowerCase() Homarr uses.
func BoardsWithSimilarName(boards []BoardSummary, name string, ignoreIDs ...string) []BoardSummary {
	var matches []BoardSummary
	for _, board := range boards {
		if !strings.EqualFold(board.Name, name) || containsString(ignoreIDs, board.ID) {
			continue
		}
		matches = append(matches, board)
	}
	return matches
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// IsBoardNameConstraintViolation reports whether err is the HTTP 500 that
// POST /api/boards answers when the name is already taken.
//
// Homarr's REST create never checks names; the insert trips the unique
// constraint on board.name, and trpc-to-openapi reports any error that is not
// a TRPCError as INTERNAL_SERVER_ERROR carrying the original message. That
// message depends on the database: better-sqlite3 throws "UNIQUE constraint
// failed: board.name", while on PostgreSQL drizzle-orm wraps the driver error
// as "Failed query: insert into "board" ...", hiding the constraint name. The
// board row is the first insert and its id is freshly generated, so a failure
// of that statement is in practice always the name.
func IsBoardNameConstraintViolation(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		return false
	}
	message := strings.ToLower(apiErr.Message)
	return strings.Contains(message, "unique constraint failed: board.name") ||
		strings.HasPrefix(message, `failed query: insert into "board" `)
}
