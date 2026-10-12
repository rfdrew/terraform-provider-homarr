package client

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// userList renders a GET /api/users response from id/name pairs.
func userList(t *testing.T, idsAndNames ...string) string {
	t.Helper()
	users := []UserSummary{}
	for i := 0; i+1 < len(idsAndNames); i += 2 {
		users = append(users, UserSummary{ID: idsAndNames[i], Name: idsAndNames[i+1]})
	}
	raw, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func aliceRequest(username string) UserCreateRequest {
	return UserCreateRequest{
		Username:        username,
		Password:        "Correct-Horse-1",
		ConfirmPassword: "Correct-Horse-1",
		GroupIDs:        []string{},
	}
}

func TestCreateUserReturnsTheNewUsersID(t *testing.T) {
	f := newFakeHomarr()
	f.replies("GET /api/users", http.StatusOK, userList(t), userList(t, "new-1", "alice"))
	f.reply("POST /api/users", http.StatusOK, "")

	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if id != "new-1" {
		t.Errorf("CreateUser() = %q, want %q", id, "new-1")
	}

	// The list has to be read before the POST, or the new user cannot be told
	// apart from the ones already there.
	wantCalls := []string{"GET /api/users", "POST /api/users", "GET /api/users"}
	if got := f.routes(); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls = %v, want %v", got, wantCalls)
	}
	body := f.body(t, "POST /api/users")
	if got := body["username"]; got != "alice" {
		t.Errorf("POST /api/users username = %v, want %q", got, "alice")
	}
	// Homarr rejects `"email": null` but requires groupIds, even when empty.
	if _, present := body["email"]; present {
		t.Errorf("POST /api/users body contains an email key, want it omitted: %v", body)
	}
	if _, present := body["groupIds"]; !present {
		t.Errorf("POST /api/users body is missing groupIds: %v", body)
	}
}

// Homarr keeps usernames unique per auth provider only, so an LDAP or OIDC
// account can share the name; GET /api/users does not say which is which. The
// older account is listed first, where a lookup by name alone would stop.
func TestCreateUserSkipsAnExistingUserOfTheSameName(t *testing.T) {
	f := newFakeHomarr()
	f.replies("GET /api/users", http.StatusOK,
		userList(t, "ldap-1", "alice"),
		userList(t, "ldap-1", "alice", "new-1", "alice"),
	)
	f.reply("POST /api/users", http.StatusOK, "")

	// Configured in mixed case, which Homarr lowercases before storing.
	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("Alice"))
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if id != "new-1" {
		t.Errorf("CreateUser() = %q, want the new user %q, not the existing one", id, "new-1")
	}
}

func TestCreateUserFailsWhenNoNewUserOfThatNameAppears(t *testing.T) {
	tests := []struct {
		name  string
		after string
	}{
		{"list unchanged", userList(t, "ldap-1", "alice")},
		{"only a differently named user is new", userList(t, "ldap-1", "alice", "new-1", "bob")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHomarr()
			f.replies("GET /api/users", http.StatusOK, userList(t, "ldap-1", "alice"), tc.after)
			f.reply("POST /api/users", http.StatusOK, "")

			id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
			if err == nil {
				t.Fatalf("CreateUser() = %q, want an error", id)
			}
			if !strings.Contains(err.Error(), "was created but could not be found afterwards") {
				t.Errorf("CreateUser() error = %v, want it to say the user was created but not found", err)
			}
			if IsNotFound(err) {
				t.Errorf("CreateUser() error = %v must not read as a 404: the user does exist", err)
			}
		})
	}
}

func TestCreateUserFailsWhenSeveralNewUsersOfThatNameAppear(t *testing.T) {
	f := newFakeHomarr()
	f.replies("GET /api/users", http.StatusOK,
		userList(t),
		userList(t, "new-1", "alice", "new-2", "alice"),
	)
	f.reply("POST /api/users", http.StatusOK, "")

	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if err == nil {
		t.Fatalf("CreateUser() = %q, want an error", id)
	}
	for _, want := range []string{"ambiguous", "new-1", "new-2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("CreateUser() error = %v, want it to contain %q", err, want)
		}
	}
}

func TestCreateUserDoesNotPostWithoutTheExistingUsers(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusForbidden, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	f.reply("POST /api/users", http.StatusOK, "")

	_, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if !IsForbidden(err) {
		t.Fatalf("CreateUser() error = %v, want the 403 from GET /api/users", err)
	}
	if f.called("POST /api/users") {
		t.Error("POST /api/users was called although the existing users could not be listed")
	}
}

// The resource maps a 409 to a "Username already taken" diagnostic, so the
// POST's error has to come back unwrapped enough for IsConflict.
func TestCreateUserPassesAConflictThrough(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusOK, userList(t, "old-1", "alice"))
	f.reply("POST /api/users", http.StatusConflict, `{"message":"Username already taken","code":"CONFLICT"}`)

	_, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if !IsConflict(err) {
		t.Fatalf("CreateUser() error = %v, want a 409", err)
	}
}

func TestSameUsername(t *testing.T) {
	tests := []struct {
		name       string
		stored     string
		configured string
		want       bool
	}{
		{"identical", "alice", "alice", true},
		{"configured in upper case", "alice", "ALICE", true},
		{"configured with surrounding whitespace", "alice", " \talice\n", true},
		{"byte order mark, which JavaScript trims and Go does not", "alice", "\ufeffalice", true},
		{"next line, which Go trims and JavaScript does not", "alice", "alice\u0085", false},
		{"next line kept by Homarr", "alice\u0085", "alice\u0085", true},
		{"final sigma, which JavaScript lowercases to ς", "οδος", "ΟΔΟΣ", true},
		{"different name", "alice", "alicia", false},
		{"inner whitespace is not trimmed", "alice", "al ice", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameUsername(tc.stored, tc.configured); got != tc.want {
				t.Errorf("sameUsername(%q, %q) = %v, want %v", tc.stored, tc.configured, got, tc.want)
			}
		})
	}
}
