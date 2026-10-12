package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const boardsWithCaseVariants = `[` +
	`{"id":"b1","name":"infra","isPublic":true,"creator":{"id":"u1","name":"admin","image":null,"email":"a@b.c"},"isHome":false,"isMobileHome":false},` +
	`{"id":"b2","name":"Infra","isPublic":false,"creator":null,"isHome":false,"isMobileHome":false},` +
	`{"id":"b3","name":"Media","isPublic":false,"creator":null,"isHome":false,"isMobileHome":false}` +
	`]`

func TestGetBoardByNamePrefersTheExactName(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/boards", http.StatusOK, boardsWithCaseVariants)

	board, err := f.client(t).GetBoardByName(context.Background(), "Infra")
	if err != nil {
		t.Fatalf("GetBoardByName() error = %v", err)
	}
	if board.ID != "b2" {
		t.Errorf("GetBoardByName(\"Infra\") = %s, want the exact match b2", board.ID)
	}
}

// Homarr resolves board URLs case-insensitively, so a name that differs only in
// case still finds the board when it is the only candidate.
func TestGetBoardByNameFallsBackToCaseInsensitive(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/boards", http.StatusOK, boardsWithCaseVariants)

	board, err := f.client(t).GetBoardByName(context.Background(), "media")
	if err != nil {
		t.Fatalf("GetBoardByName() error = %v", err)
	}
	if board.ID != "b3" {
		t.Errorf("GetBoardByName(\"media\") = %s, want b3", board.ID)
	}
}

func TestGetBoardByNameRefusesToGuessBetweenCaseVariants(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/boards", http.StatusOK, boardsWithCaseVariants)

	_, err := f.client(t).GetBoardByName(context.Background(), "INFRA")
	var ambiguous *AmbiguousBoardNameError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("GetBoardByName(\"INFRA\") error = %v, want an AmbiguousBoardNameError", err)
	}
	if len(ambiguous.Matches) != 2 || !strings.Contains(err.Error(), "b1") || !strings.Contains(err.Error(), "b2") {
		t.Errorf("error = %v, want both case variants named", err)
	}
	if IsNotFound(err) {
		t.Error("an ambiguous name was reported as not found")
	}
}

func TestGetBoardByNameReportsAMissingBoardAsNotFound(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/boards", http.StatusOK, boardsWithCaseVariants)

	_, err := f.client(t).GetBoardByName(context.Background(), "nope")
	if !IsNotFound(err) {
		t.Fatalf("GetBoardByName(\"nope\") error = %v, want a 404", err)
	}
}

func TestBoardsWithSimilarNameSkipsIgnoredIDs(t *testing.T) {
	boards := []BoardSummary{{ID: "b1", Name: "infra"}, {ID: "b2", Name: "Infra"}, {ID: "b3", Name: "infra-2"}}

	got := BoardsWithSimilarName(boards, "INFRA", "b1")
	if len(got) != 1 || got[0].ID != "b2" {
		t.Fatalf("BoardsWithSimilarName() = %+v, want only b2", got)
	}
}

func TestCreateBoardReturnsTheBaseLayoutID(t *testing.T) {
	f := newFakeHomarr()
	f.reply("POST /api/boards", http.StatusOK, `{"boardId":"b1","name":"infra","layoutId":"l1"}`)

	created, err := f.client(t).CreateBoard(context.Background(), BoardCreateRequest{Name: "infra", ColumnCount: 12})
	if err != nil {
		t.Fatalf("CreateBoard() error = %v", err)
	}
	if created.BoardID != "b1" || created.LayoutID != "l1" {
		t.Errorf("CreateBoard() = %+v, want board b1 with layout l1", created)
	}
}

func TestIsBoardNameConstraintViolation(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{
			"sqlite",
			http.StatusInternalServerError,
			`{"message":"UNIQUE constraint failed: board.name","code":"INTERNAL_SERVER_ERROR","data":{"code":"INTERNAL_SERVER_ERROR","httpStatus":500}}`,
			true,
		},
		{
			"postgresql via drizzle",
			http.StatusInternalServerError,
			`{"message":"Failed query: insert into \"board\" (\"id\", \"name\", \"is_public\", \"creator_id\") values ($1, $2, $3, $4)\nparams: b1,infra,false,u1","code":"INTERNAL_SERVER_ERROR"}`,
			true,
		},
		{
			"a later insert",
			http.StatusInternalServerError,
			`{"message":"Failed query: insert into \"layout\" (\"id\") values ($1)\nparams: l1","code":"INTERNAL_SERVER_ERROR"}`,
			false,
		},
		{
			"another server error",
			http.StatusInternalServerError,
			`{"message":"database is locked","code":"INTERNAL_SERVER_ERROR"}`,
			false,
		},
		{
			"same text, other status",
			http.StatusBadRequest,
			`{"message":"UNIQUE constraint failed: board.name","code":"BAD_REQUEST"}`,
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHomarr()
			f.reply("POST /api/boards", tc.status, tc.body)

			_, err := f.client(t).CreateBoard(context.Background(), BoardCreateRequest{Name: "infra", ColumnCount: 10})
			if got := IsBoardNameConstraintViolation(err); got != tc.want {
				t.Errorf("IsBoardNameConstraintViolation(%v) = %v, want %v", err, got, tc.want)
			}
		})
	}
}

func TestDuplicateBoardSendsTheNameAndReturnsTheCopy(t *testing.T) {
	f := newFakeHomarr()
	f.reply("POST /api/boards/src/duplicate", http.StatusOK, `{"boardId":"copy"}`)

	id, err := f.client(t).DuplicateBoard(context.Background(), "src", "team-a")
	if err != nil {
		t.Fatalf("DuplicateBoard() error = %v", err)
	}
	if id != "copy" {
		t.Errorf("DuplicateBoard() = %q, want copy", id)
	}
	if body := f.body(t, "POST /api/boards/src/duplicate"); body["name"] != "team-a" {
		t.Errorf("duplicate body = %v, want name team-a", body)
	}
}

func TestAddBoardItemSendsDefaultsForOmittedFields(t *testing.T) {
	f := newFakeHomarr()
	f.reply("POST /api/boards/items", http.StatusOK, `{"itemId":"i1"}`)

	id, err := f.client(t).AddBoardItem(context.Background(), BoardItemCreateRequest{BoardID: "b1", Kind: "clock"})
	if err != nil {
		t.Fatalf("AddBoardItem() error = %v", err)
	}
	if id != "i1" {
		t.Errorf("AddBoardItem() = %q, want i1", id)
	}

	body := f.body(t, "POST /api/boards/items")
	// Homarr's options record and integration list reject null; size is
	// optional and must be absent rather than null.
	if options, ok := body["options"].(map[string]any); !ok || len(options) != 0 {
		t.Errorf("options = %#v, want {}", body["options"])
	}
	if ids, ok := body["integrationIds"].([]any); !ok || len(ids) != 0 {
		t.Errorf("integrationIds = %#v, want []", body["integrationIds"])
	}
	if _, ok := body["size"]; ok {
		t.Errorf("size = %#v, want it omitted", body["size"])
	}
}

func TestAddBoardItemPassesOptionsAndSizeThrough(t *testing.T) {
	f := newFakeHomarr()
	f.reply("POST /api/boards/items", http.StatusOK, `{"itemId":"i1"}`)

	_, err := f.client(t).AddBoardItem(context.Background(), BoardItemCreateRequest{
		BoardID:        "b1",
		Kind:           "app",
		Options:        []byte(`{"appId":"a1","showTitle":false}`),
		Size:           &BoardItemSize{Width: 2, Height: 1},
		IntegrationIDs: []string{"i9"},
	})
	if err != nil {
		t.Fatalf("AddBoardItem() error = %v", err)
	}

	body := f.body(t, "POST /api/boards/items")
	options, _ := body["options"].(map[string]any)
	if options["appId"] != "a1" || options["showTitle"] != false {
		t.Errorf("options = %#v, want the configured object", body["options"])
	}
	size, _ := body["size"].(map[string]any)
	if size["width"] != float64(2) || size["height"] != float64(1) {
		t.Errorf("size = %#v, want 2x1", body["size"])
	}
}

func TestMoveBoardItemSendsThePlacement(t *testing.T) {
	f := newFakeHomarr()
	route := "PATCH /api/boards/b1/items/i1/layouts/l1"
	f.reply(route, http.StatusOK, `{"boardId":"b1","itemId":"i1","layoutId":"l1","xOffset":4,"yOffset":0,"width":2,"height":1}`)

	err := f.client(t).MoveBoardItem(context.Background(), "b1", "i1", "l1", BoardItemLayout{XOffset: 4, YOffset: 0, Width: 2, Height: 1})
	if err != nil {
		t.Fatalf("MoveBoardItem() error = %v", err)
	}

	body := f.body(t, route)
	// The ids travel in the path; all four placement fields are required, so
	// a zero offset must be sent rather than omitted.
	if body["xOffset"] != float64(4) || body["yOffset"] != float64(0) || body["width"] != float64(2) || body["height"] != float64(1) {
		t.Errorf("body = %#v, want the full placement", body)
	}
}
