package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestUpdateUserProfileSendsTheIDNameAndEmail(t *testing.T) {
	f := newFakeHomarr()
	f.reply("PUT /api/users/profile", http.StatusOK, "")

	email := "alice@example.com"
	if err := f.client(t).UpdateUserProfile(context.Background(), "u1", "alice", &email); err != nil {
		t.Fatalf("UpdateUserProfile() error = %v", err)
	}
	want := map[string]any{"id": "u1", "name": "alice", "email": "alice@example.com"}
	if got := f.body(t, "PUT /api/users/profile"); !reflect.DeepEqual(got, want) {
		t.Errorf("PUT /api/users/profile body = %v, want %v", got, want)
	}
}

// Homarr requires the name on every profile update, but an absent email leaves
// the stored one alone.
func TestUpdateUserProfileOmitsANilEmail(t *testing.T) {
	f := newFakeHomarr()
	f.reply("PUT /api/users/profile", http.StatusOK, "")

	if err := f.client(t).UpdateUserProfile(context.Background(), "u1", "alice", nil); err != nil {
		t.Fatalf("UpdateUserProfile() error = %v", err)
	}
	body := f.body(t, "PUT /api/users/profile")
	if _, present := body["email"]; present {
		t.Errorf("PUT /api/users/profile body = %v, want no email key", body)
	}
	if body["name"] != "alice" {
		t.Errorf("PUT /api/users/profile body = %v, want the name sent", body)
	}
}

// Homarr inserts the user before its group memberships, without a transaction,
// so a bad group id fails the POST after the account exists.
func TestCreateUserRecoversTheAccountAFailedPostLeftBehind(t *testing.T) {
	f := newFakeHomarr()
	f.replies("GET /api/users", http.StatusOK, userList(t), userList(t, "new-1", "alice"))
	f.reply("POST /api/users", http.StatusInternalServerError, `{"message":"FOREIGN KEY constraint failed","code":"INTERNAL_SERVER_ERROR"}`)

	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if id != "new-1" {
		t.Errorf("CreateUser() id = %q, want the account the failed POST created", id)
	}
	if err == nil {
		t.Fatal("CreateUser() error = nil, want the POST's failure reported")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("CreateUser() error = %v, want it to wrap the 500", err)
	}
	if !strings.Contains(err.Error(), "was created") {
		t.Errorf("CreateUser() error = %v, want it to say the user exists", err)
	}
}

func TestCreateUserReturnsNoIDWhenAFailedPostCreatedNothing(t *testing.T) {
	tests := []struct {
		name  string
		after string
	}{
		{"nothing new", userList(t, "old-1", "bob")},
		{"two new users of that name", userList(t, "old-1", "bob", "new-1", "alice", "new-2", "alice")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHomarr()
			f.replies("GET /api/users", http.StatusOK, userList(t, "old-1", "bob"), tc.after)
			f.reply("POST /api/users", http.StatusBadRequest, `{"message":"Invalid input","code":"BAD_REQUEST"}`)

			id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
			if id != "" {
				t.Errorf("CreateUser() id = %q, want none", id)
			}
			var apiErr *Error
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
				t.Errorf("CreateUser() error = %v, want the 400 unchanged", err)
			}
		})
	}
}

func TestCreateUserKeepsThePostErrorWhenTheRecoveryCannotList(t *testing.T) {
	f := newFakeHomarr()
	var mu sync.Mutex
	listed := 0
	f.handle("GET /api/users", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		listed++
		if listed > 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, userList(t))
	})
	f.reply("POST /api/users", http.StatusInternalServerError, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)

	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if id != "" {
		t.Errorf("CreateUser() id = %q, want none", id)
	}
	if !strings.Contains(fmtError(err), "boom") {
		t.Errorf("CreateUser() error = %v, want the POST's own error", err)
	}
}

// A 409 means the name was taken before anything was written, so there is
// nothing to recover and no second listing.
func TestCreateUserDoesNotLookForAnAccountAfterAConflict(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/users", http.StatusOK, userList(t, "old-1", "alice"))
	f.reply("POST /api/users", http.StatusConflict, `{"message":"Username already taken","code":"CONFLICT"}`)

	if _, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice")); !IsConflict(err) {
		t.Fatalf("CreateUser() error = %v, want a 409", err)
	}
	want := []string{"GET /api/users", "POST /api/users"}
	if got := f.routes(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// users.name is nullable; a null must neither break decoding nor be taken for
// the new user.
func TestCreateUserIgnoresUsersWithoutAName(t *testing.T) {
	f := newFakeHomarr()
	f.replies("GET /api/users", http.StatusOK, `[]`, `[{"id":"nameless","name":null},{"id":"new-1","name":"alice"}]`)
	f.reply("POST /api/users", http.StatusOK, "")

	id, err := f.client(t).CreateUser(context.Background(), aliceRequest("alice"))
	if err != nil || id != "new-1" {
		t.Fatalf("CreateUser() = %q, %v; want new-1", id, err)
	}
}

func TestUserNamesDecodeNullAsNil(t *testing.T) {
	var users []UserSummary
	if err := json.Unmarshal([]byte(`[{"id":"u1","name":null},{"id":"u2","name":"bob"}]`), &users); err != nil {
		t.Fatal(err)
	}
	if users[0].Name != nil || users[1].Name == nil || *users[1].Name != "bob" {
		t.Errorf("names = %v, %v; want nil and bob", users[0].Name, users[1].Name)
	}

	var detail UserDetail
	if err := json.Unmarshal([]byte(`{"id":"u1","name":null,"provider":"oidc"}`), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Name != nil {
		t.Errorf("UserDetail.Name = %v, want nil", *detail.Name)
	}
}

func TestCurrentUserIDAsksForTheCallersPreferences(t *testing.T) {
	f := newFakeHomarr()
	f.handle("GET /api/users/preferences", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("GET /api/users/preferences query = %q, want none so that Homarr picks the caller", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"userId":"me","colorScheme":"dark"}`)
	})

	id, err := f.client(t).CurrentUserID(context.Background())
	if err != nil || id != "me" {
		t.Fatalf("CurrentUserID() = %q, %v; want me", id, err)
	}
}

func TestCurrentUserIDPassesA404Through(t *testing.T) {
	f := newFakeHomarr() // a Homarr older than 2.3.0
	if _, err := f.client(t).CurrentUserID(context.Background()); !IsNotFound(err) {
		t.Fatalf("CurrentUserID() error = %v, want the 404", err)
	}
}

func TestUserPreferencesDecodeTheHeader(t *testing.T) {
	var prefs UserPreferences
	raw := `{"userId":"u1","colorScheme":"dark","headerPreferences":{"version":3,"visible":false,` +
		`"searchDisplay":"icon","logoDisplay":"logo","zones":{"left":[{"type":"builtin","id":"logo"}],` +
		`"center":[],"right":[{"type":"board","boardId":"b1"},{"type":"builtin","id":"user"}]}}}`
	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		t.Fatal(err)
	}
	want := &HeaderPreferences{
		Version:       3,
		Visible:       false,
		SearchDisplay: "icon",
		LogoDisplay:   "logo",
		Zones: HeaderZones{
			Left:   []HeaderItem{{Type: "builtin", ID: "logo"}},
			Center: []HeaderItem{},
			Right:  []HeaderItem{{Type: "board", BoardID: "b1"}, {Type: "builtin", ID: "user"}},
		},
	}
	if !reflect.DeepEqual(prefs.HeaderPreferences, want) {
		t.Errorf("HeaderPreferences = %+v, want %+v", prefs.HeaderPreferences, want)
	}
	if prefs.ColorScheme != "dark" {
		t.Errorf("ColorScheme = %q, want the shared fields still decoded", prefs.ColorScheme)
	}
}

// GET /api/users/{id} reports the header as the stored JSON text, which must
// not break decoding the rest of the user.
func TestUserDetailIgnoresTheRawHeaderText(t *testing.T) {
	var detail UserDetail
	raw := `{"id":"u1","name":"alice","provider":"credentials","headerPreferences":"{\"version\":1}","colorScheme":"light"}`
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("decoding a user with a raw header: %v", err)
	}
	if detail.ColorScheme != "light" {
		t.Errorf("ColorScheme = %q, want light", detail.ColorScheme)
	}
}

// Homarr's strict item schemas reject a field of the other item type, and its
// zone schemas reject null.
func TestHeaderPreferencesEncodeAsHomarrExpects(t *testing.T) {
	raw, err := json.Marshal(HeaderPreferences{
		Version:       HeaderPreferencesVersion,
		Visible:       true,
		SearchDisplay: "input",
		LogoDisplay:   "logoAndText",
		Zones:         HeaderZones{Right: []HeaderItem{{Type: "board", BoardID: "b1"}, {Type: "builtin", ID: "user"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":3,"visible":true,"searchDisplay":"input","logoDisplay":"logoAndText",` +
		`"zones":{"left":[],"center":[],"right":[{"type":"board","boardId":"b1"},{"type":"builtin","id":"user"}]}}`
	if string(raw) != want {
		t.Errorf("encoded header =\n%s\nwant\n%s", raw, want)
	}
}

func fmtError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
