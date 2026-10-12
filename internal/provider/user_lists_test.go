package provider

import (
	"context"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// These tests cover the lists of users a key that is not an admin's may read:
// homarr_users, and the summary the homarr_user data source falls back to.

// selectableByProvider answers GET /api/users/selectable with the body for the
// requested providers filter, "" meaning none.
func selectableByProvider(fake *fakeHomarr, bodies map[string]string) {
	fake.handle("GET /api/users/selectable", func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Query().Get("providers")]
		if !ok {
			body = `[]`
		}
		_, _ = io.WriteString(w, body)
	})
}

func usersDataSourceState(t *testing.T, fake *fakeHomarr, config usersDataSourceModel) usersDataSourceModel {
	t.Helper()
	resp := runDataSourceRead(t, &usersDataSource{client: fake.client(t)}, config)
	requireNoErrors(t, resp.Diagnostics)
	var state usersDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	return state
}

func providersOf(users []userSummaryModel) map[string]types.String {
	providers := map[string]types.String{}
	for _, u := range users {
		providers[u.ID.ValueString()] = u.Provider
	}
	return providers
}

func TestUsersDataSourceReportsEachUsersProvider(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"c1","name":"alice"},{"id":"o1","name":"Alice"},{"id":"x1","name":"zed"}]`)
	selectableByProvider(fake, map[string]string{
		"credentials": `[{"id":"c1","name":"alice"}]`,
		"oidc":        `[{"id":"o1","name":"Alice"}]`,
	})

	state := usersDataSourceState(t, fake, usersDataSourceModel{})
	got := providersOf(state.Users)
	// x1 signs in through a provider Homarr may add later.
	want := map[string]types.String{"c1": types.StringValue("credentials"), "o1": types.StringValue("oidc"), "x1": types.StringNull()}
	for id, provider := range want {
		if !got[id].Equal(provider) {
			t.Errorf("provider_name of %s = %v, want %v", id, got[id], provider)
		}
	}
}

// The UI saves a blank email as "", which the other user reads report as null.
func TestUsersDataSourceReadsAnEmptyEmailAsNone(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"c1","name":"alice","email":""},{"id":"c2","name":"bob","email":"bob@example.com"}]`)
	selectableByProvider(fake, map[string]string{})

	state := usersDataSourceState(t, fake, usersDataSourceModel{})
	emails := map[string]types.String{}
	for _, u := range state.Users {
		emails[u.ID.ValueString()] = u.Email
	}
	if !emails["c1"].IsNull() || emails["c2"].ValueString() != "bob@example.com" {
		t.Fatalf("emails = %v, want null for alice and bob's address", emails)
	}
}

func TestUsersDataSourceFallsBackForAKeyThatIsNotAnAdmins(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 403, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	selectableByProvider(fake, map[string]string{
		"":     `[{"id":"c1","name":"alice","email":"alice@example.com"}]`,
		"ldap": `[{"id":"c1","name":"alice","email":"alice@example.com"}]`,
	})

	state := usersDataSourceState(t, fake, usersDataSourceModel{})
	if len(state.Users) != 1 || state.Users[0].Email.ValueString() != "alice@example.com" {
		t.Fatalf("users = %v, want the selectable list", state.Users)
	}
	if state.Users[0].Provider.ValueString() != "ldap" {
		t.Errorf("provider_name = %v, want ldap", state.Users[0].Provider)
	}
}

func TestUsersDataSourceListsOneProvider(t *testing.T) {
	fake := newFakeHomarr()
	selectableByProvider(fake, map[string]string{"oidc": `[{"id":"o2","name":"bob"},{"id":"o1","name":"alice"}]`})

	state := usersDataSourceState(t, fake, usersDataSourceModel{Provider: types.StringValue("oidc")})
	var ids []string
	for _, u := range state.Users {
		ids = append(ids, u.ID.ValueString())
		if u.Provider.ValueString() != "oidc" {
			t.Errorf("provider_name of %v = %v, want oidc", u.ID, u.Provider)
		}
	}
	if want := []string{"o1", "o2"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if calls := fake.callLog(); len(calls) != 1 {
		t.Errorf("calls = %v, want the one filtered list", calls)
	}
}

// --- homarr_user for a key that is not an admin's -------------------------------

func TestUserDataSourceReportsTheSummaryForAKeyThatIsNotAnAdmins(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/o1", 403, legacyPreferencesRefusal)
	selectableByProvider(fake, map[string]string{
		"oidc": `[{"id":"o1","name":"Bob","email":"bob@example.com","image":"https://idp.example.com/bob.png"}]`,
	})
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("o1", "", ""))
	state := userDataSourceState(t, resp)

	requireWarning(t, resp.Diagnostics, "not an admin")
	if state.Username.ValueString() != "Bob" || state.Provider.ValueString() != "oidc" ||
		state.Email.ValueString() != "bob@example.com" || state.Image.IsNull() {
		t.Errorf("summary = %+v, want the selectable entry and its provider", state)
	}
	if !state.ColorScheme.IsNull() || !state.Header.IsNull() {
		t.Errorf("color_scheme = %v, header = %v; want null", state.ColorScheme, state.Header)
	}
}

func TestUserDataSourceReportsAUserMissingFromTheSummary(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u9", 403, legacyPreferencesRefusal)
	selectableByProvider(fake, map[string]string{})
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("u9", "", ""))
	requireErrorContaining(t, resp.Diagnostics, "no user with id u9")
}
