package client

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"testing"
)

// selectableUsers answers GET /api/users/selectable with the users of the
// requested provider, recording the query each request carried.
func selectableUsers(t *testing.T, f *fakeHomarr, byProvider map[string]string) *[]string {
	t.Helper()
	var queries []string
	f.handle("GET /api/users/selectable", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		body, ok := byProvider[r.URL.Query().Get("providers")]
		if !ok {
			t.Errorf("unexpected providers filter %q", r.URL.RawQuery)
			body = "[]"
		}
		_, _ = io.WriteString(w, body)
	})
	return &queries
}

func TestListSelectableUsersFiltersByProviderOnTheServer(t *testing.T) {
	f := newFakeHomarr()
	queries := selectableUsers(t, f, map[string]string{
		"":     userList(t, "c1", "alice", "o1", "alice"),
		"oidc": userList(t, "o1", "alice"),
	})
	c := f.client(t)

	all, err := c.ListSelectableUsers(context.Background(), "")
	if err != nil {
		t.Fatalf("ListSelectableUsers() error = %v", err)
	}
	oidc, err := c.ListSelectableUsers(context.Background(), "oidc")
	if err != nil {
		t.Fatalf("ListSelectableUsers(oidc) error = %v", err)
	}

	if len(all) != 2 || len(oidc) != 1 || oidc[0].ID != "o1" {
		t.Errorf("ListSelectableUsers() = %v and %v, want both users and then only o1", all, oidc)
	}
	// No filter means no query at all; a filter is a single providers value,
	// which trpc-to-openapi turns into the one-element array Homarr expects.
	if want := []string{"", "providers=oidc"}; !reflect.DeepEqual(*queries, want) {
		t.Errorf("queries = %q, want %q", *queries, want)
	}
}

// Homarr trims and lowercases usernames, so a lookup ignores both; a user
// whose name is merely similar, or who has none, never matches.
func TestFindUsersByNameMatchesLikeASignIn(t *testing.T) {
	f := newFakeHomarr()
	selectableUsers(t, f, map[string]string{
		"": `[{"id":"c1","name":"alice"},{"id":"x1","name":"alice2"},{"id":"n1","name":null},` +
			`{"id":"o1","name":"Alice","email":"alice@example.com"}]`,
	})

	matches, err := f.client(t).FindUsersByName(context.Background(), " ALICE ", "")
	if err != nil {
		t.Fatalf("FindUsersByName() error = %v", err)
	}
	var ids []string
	for _, u := range matches {
		ids = append(ids, u.ID)
	}
	if want := []string{"c1", "o1"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("FindUsersByName() ids = %v, want %v", ids, want)
	}
	if matches[1].Email == nil || *matches[1].Email != "alice@example.com" {
		t.Errorf("FindUsersByName() did not decode the email: %+v", matches[1])
	}
}

func TestFindUsersByNameReportsAFailedListing(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users/selectable", http.StatusUnauthorized, `{"message":"Unauthorized","code":"UNAUTHORIZED"}`)

	if _, err := f.client(t).FindUsersByName(context.Background(), "alice", ""); err == nil {
		t.Fatal("FindUsersByName() error = nil, want the 401 reported")
	}
}
