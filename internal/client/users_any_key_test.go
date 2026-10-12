package client

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The 403 a Homarr before 2.3.0 answers to GET /api/users/preferences for a key
// that is not an admin's: the request lands on GET /api/users/{userId}.
const legacyPreferencesRefusal = `{"message":"You are not allowed to view other users details","code":"FORBIDDEN"}`

func TestGetUserPreferencesReadsTheLegacyRefusalAsAMissingEndpoint(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users/preferences", http.StatusForbidden, legacyPreferencesRefusal)

	_, err := f.client(t).GetUserPreferences(context.Background(), "")
	if !IsNotFound(err) {
		t.Fatalf("GetUserPreferences() error = %v, want a 404 like an admin key gets", err)
	}
	if !strings.Contains(err.Error(), "predates 2.3.0") {
		t.Errorf("GetUserPreferences() error = %v, want it to name the version", err)
	}

	if _, err := f.client(t).CurrentUserID(context.Background()); !IsNotFound(err) {
		t.Errorf("CurrentUserID() error = %v, want the same 404", err)
	}
}

func TestGetUserPreferencesKeepsAnyOtherRefusal(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users/preferences", http.StatusForbidden, `{"message":"Forbidden","code":"FORBIDDEN"}`)

	if _, err := f.client(t).GetUserPreferences(context.Background(), "u1"); !IsForbidden(err) {
		t.Fatalf("GetUserPreferences() error = %v, want the 403 kept", err)
	}
}

// Homarr's schema makes image required but nullable, so removing a picture is
// an explicit null rather than an absent key.
func TestSetUserProfileImageSendsAnExplicitNull(t *testing.T) {
	f := newFakeHomarr()
	f.reply("PUT /api/users/profileImage", http.StatusOK, "")
	c := f.client(t)

	image := "data:image/png;base64,iVBORw0KGgo="
	if err := c.SetUserProfileImage(context.Background(), "u1", &image); err != nil {
		t.Fatalf("SetUserProfileImage() error = %v", err)
	}
	if got, want := f.body(t, "PUT /api/users/profileImage"), map[string]any{"userId": "u1", "image": image}; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}

	if err := c.SetUserProfileImage(context.Background(), "u1", nil); err != nil {
		t.Fatalf("SetUserProfileImage(nil) error = %v", err)
	}
	if got, want := f.body(t, "PUT /api/users/profileImage"), map[string]any{"userId": "u1", "image": nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestListUsersWithAnyKeyFallsBackForAKeyThatIsNotAnAdmins(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusForbidden, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	f.reply("GET /api/users/selectable", http.StatusOK, userList(t, "u1", "alice"))

	users, err := f.client(t).ListUsersWithAnyKey(context.Background())
	if err != nil {
		t.Fatalf("ListUsersWithAnyKey() error = %v", err)
	}
	if len(users) != 1 || users[0].ID != "u1" {
		t.Errorf("ListUsersWithAnyKey() = %v, want the selectable users", users)
	}
}

func TestListUsersWithAnyKeyReportsTheOriginalRefusal(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusForbidden, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	f.reply("GET /api/users/selectable", http.StatusInternalServerError, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)

	_, err := f.client(t).ListUsersWithAnyKey(context.Background())
	if !IsForbidden(err) {
		t.Fatalf("ListUsersWithAnyKey() error = %v, want the 403 from GET /api/users", err)
	}
}

func TestListUsersWithAnyKeyOnlyFallsBackOnARefusal(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusInternalServerError, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	f.reply("GET /api/users/selectable", http.StatusOK, userList(t, "u1", "alice"))

	if _, err := f.client(t).ListUsersWithAnyKey(context.Background()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ListUsersWithAnyKey() error = %v, want the 500 reported", err)
	}
	if f.called("GET /api/users/selectable") {
		t.Error("fell back on an error that is not a refusal")
	}
}

func TestListUsersByProviderLabelsEveryUser(t *testing.T) {
	f := newFakeHomarr()
	f.handle("GET /api/users/selectable", func(w http.ResponseWriter, r *http.Request) {
		bodies := map[string]string{
			"credentials": userList(t, "c1", "alice"),
			"oidc":        userList(t, "o1", "Alice", "o2", "bob"),
			"ldap":        userList(t),
		}
		_, _ = io.WriteString(w, bodies[r.URL.Query().Get("providers")])
	})

	users, err := f.client(t).ListUsersByProvider(context.Background())
	if err != nil {
		t.Fatalf("ListUsersByProvider() error = %v", err)
	}
	got := map[string]string{}
	for _, u := range users {
		got[u.ID] = u.Provider
	}
	if want := map[string]string{"c1": "credentials", "o1": "oidc", "o2": "oidc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("providers = %v, want %v", got, want)
	}
}
