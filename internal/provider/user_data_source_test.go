package provider

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests drive the homarr_user, homarr_current_user and
// homarr_user_preferences data sources against the in-process fake Homarr from
// fallbacks_test.go: lookups, ambiguous names, refused reads and releases
// older than 2.3.0, which the acceptance suite cannot set up.

func dataSourceSchemaOf(t *testing.T, d datasource.DataSource) dsschema.Schema {
	t.Helper()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	return resp.Schema
}

// dataSourceConfigOf builds the configuration a data source is read with.
func dataSourceConfigOf(t *testing.T, s dsschema.Schema, model any) tfsdk.Config {
	t.Helper()
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	if diags := state.Set(context.Background(), model); diags.HasError() {
		t.Fatalf("building config: %v", diags)
	}
	return tfsdk.Config{Schema: s, Raw: state.Raw}
}

func runDataSourceRead(t *testing.T, d datasource.DataSource, config any) datasource.ReadResponse {
	t.Helper()
	s := dataSourceSchemaOf(t, d)
	cfg := dataSourceConfigOf(t, s, config)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: cfg.Raw}}
	d.Read(context.Background(), datasource.ReadRequest{Config: cfg}, &resp)
	return resp
}

// userLookup is the configuration of a homarr_user data source; an empty
// argument is left out.
func userLookup(id, username, providerName string) userDataSourceModel {
	m := userDataSourceModel{userPreferenceValues: userPreferenceValues{Header: types.ObjectNull(headerAttrTypes)}}
	if id != "" {
		m.ID = types.StringValue(id)
	}
	if username != "" {
		m.Username = types.StringValue(username)
	}
	if providerName != "" {
		m.Provider = types.StringValue(providerName)
	}
	return m
}

func userDataSourceState(t *testing.T, resp datasource.ReadResponse) userDataSourceModel {
	t.Helper()
	requireNoErrors(t, resp.Diagnostics)
	var state userDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	return state
}

// preferencesQueries answers GET /api/users/preferences with body and records
// the query of every request, so tests can tell which user was asked for.
func preferencesQueries(fake *fakeHomarr, status int, body string) func() []string {
	var mu sync.Mutex
	var queries []string
	fake.handle("GET /api/users/preferences", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), queries...)
	}
}

func requireWarning(t *testing.T, diags diag.Diagnostics, want string) {
	t.Helper()
	for _, d := range diags.Warnings() {
		if strings.Contains(d.Summary(), want) || strings.Contains(d.Detail(), want) {
			return
		}
	}
	t.Fatalf("expected a warning containing %q, got %v", want, diags)
}

func zoneLength(t *testing.T, header types.Object, zone string) int {
	t.Helper()
	list, ok := header.Attributes()[zone].(types.List)
	if !ok || list.IsNull() || list.IsUnknown() {
		t.Fatalf("header.%s = %v, want a known list", zone, header.Attributes()[zone])
	}
	return len(list.Elements())
}

// --- schemas -----------------------------------------------------------------

func TestUserDataSourceSchemasAreValid(t *testing.T) {
	for _, d := range []datasource.DataSource{&userDataSource{}, &currentUserDataSource{}, &userPreferencesDataSource{}} {
		if diags := dataSourceSchemaOf(t, d).ValidateImplementation(context.Background()); diags.HasError() {
			t.Errorf("%T schema is invalid: %v", d, diags)
		}
	}
}

// The data sources report the header and the other preferences in the types
// the resources take, so a value read from one user can be assigned to another.
func TestUserDataSourcePreferencesHaveTheResourceTypes(t *testing.T) {
	resourceAttrs := resourceSchema(t, &userPreferencesResource{}).Attributes
	for _, d := range []datasource.DataSource{&userDataSource{}, &currentUserDataSource{}, &userPreferencesDataSource{}} {
		attrs := dataSourceSchemaOf(t, d).Attributes
		for name := range userPreferenceAttributes() {
			got, want := attrs[name].GetType(), resourceAttrs[name].GetType()
			if !got.Equal(want) {
				t.Errorf("%T %s is %s, want the resource's %s", d, name, got, want)
			}
		}
		for _, name := range []string{"home_board_id", "mobile_home_board_id", "default_search_engine_id"} {
			if _, ok := attrs[name]; !ok {
				t.Errorf("%T lacks %s", d, name)
			}
		}
	}
}

// --- homarr_user: lookups -----------------------------------------------------

func TestUserDataSourceLooksUpByID(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	queries := preferencesQueries(fake, 200, userPreferencesJSON)
	d := &userDataSource{client: fake.client(t)}

	state := userDataSourceState(t, runDataSourceRead(t, d, userLookup("u1", "", "")))

	if state.ID.ValueString() != "u1" || state.Username.ValueString() != "alice" ||
		state.Provider.ValueString() != "credentials" {
		t.Errorf("id, username, provider_name = %v, %v, %v; want u1, alice, credentials",
			state.ID, state.Username, state.Provider)
	}
	if !state.Email.IsNull() || !state.Image.IsNull() || !state.HomeBoardID.IsNull() {
		t.Errorf("email, image, home_board_id = %v, %v, %v; want null for values the user does not have",
			state.Email, state.Image, state.HomeBoardID)
	}
	if state.ColorScheme.ValueString() != "dark" || state.FirstDayOfWeek.ValueInt64() != 1 {
		t.Errorf("color_scheme = %v, first_day_of_week = %v; want dark and 1", state.ColorScheme, state.FirstDayOfWeek)
	}
	// The header comes parsed from the preferences endpoint, not as the legacy
	// text GET /api/users/u1 carries, and an empty zone is [] rather than null.
	if zoneLength(t, state.Header, "left") != 1 || zoneLength(t, state.Header, "center") != 0 ||
		zoneLength(t, state.Header, "right") != 1 {
		t.Errorf("header = %v, want logo, an empty center and user", state.Header)
	}
	if got := queries(); !slices.Equal(got, []string{"userId=u1"}) {
		t.Errorf("preferences queries = %q, want the user named", got)
	}
	if fake.called("GET /api/users/selectable") {
		t.Error("listed users for a lookup by id")
	}
}

func TestUserDataSourceLooksUpByUsernameIgnoringCase(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/selectable", 200, `[{"id":"u2","name":"bob"},{"id":"u1","name":"alice"}]`)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	d := &userDataSource{client: fake.client(t)}

	state := userDataSourceState(t, runDataSourceRead(t, d, userLookup("", "Alice", "")))

	if state.ID.ValueString() != "u1" {
		t.Errorf("id = %v, want u1", state.ID)
	}
	if state.Username.ValueString() != "alice" {
		t.Errorf("username = %v, want the name as Homarr stores it", state.Username)
	}
	if fake.called("GET /api/users") {
		t.Error("used the admin-only GET /api/users to look up a name")
	}
}

func TestUserDataSourceRefusesAUsernameSharedAcrossProviders(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/selectable", 200, `[{"id":"o1","name":"admin"},{"id":"c1","name":"admin"}]`)
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("", "admin", ""))

	requireErrorContaining(t, resp.Diagnostics, "Set provider_name")
	requireErrorContaining(t, resp.Diagnostics, "c1, o1")
	for _, call := range fake.callLog() {
		if strings.HasPrefix(call, "GET /api/users/") && call != "GET /api/users/selectable" {
			t.Errorf("read a user although the name was ambiguous: %s", call)
		}
	}
}

func TestUserDataSourceNarrowsAUsernameToOneProvider(t *testing.T) {
	fake := newFakeHomarr()
	var query string
	fake.handle("GET /api/users/selectable", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"o1","name":"admin"}]`)
	})
	fake.reply("GET /api/users/o1", 200, `{"id":"o1","name":"admin","email":"admin@example.com","provider":"oidc",`+
		`"image":"https://idp.example.com/admin.png","colorScheme":"auto","byteUnitSystem":"decimal","firstDayOfWeek":0}`)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	d := &userDataSource{client: fake.client(t)}

	state := userDataSourceState(t, runDataSourceRead(t, d, userLookup("", "admin", "oidc")))

	if query != "providers=oidc" {
		t.Errorf("selectable query = %q, want the provider filtered on the server", query)
	}
	if state.ID.ValueString() != "o1" || state.Provider.ValueString() != "oidc" {
		t.Errorf("id = %v, provider_name = %v; want the OIDC account", state.ID, state.Provider)
	}
	if state.Email.ValueString() != "admin@example.com" || state.Image.ValueString() != "https://idp.example.com/admin.png" {
		t.Errorf("email = %v, image = %v; want them reported", state.Email, state.Image)
	}
}

func TestUserDataSourceReportsAnUnknownUsername(t *testing.T) {
	for _, tc := range []struct{ provider, want string }{
		{"", `No Homarr user is named "carol"`},
		{"ldap", `No Homarr user signing in through ldap is named "carol"`},
	} {
		t.Run("provider="+tc.provider, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("GET /api/users/selectable", 200, `[{"id":"u1","name":"alice"},{"id":"n1","name":null}]`)
			d := &userDataSource{client: fake.client(t)}

			resp := runDataSourceRead(t, d, userLookup("", "carol", tc.provider))
			requireErrorContaining(t, resp.Diagnostics, tc.want)
			for _, e := range resp.Diagnostics.Errors() {
				if withPath, ok := e.(diag.DiagnosticWithPath); !ok || !withPath.Path().Equal(path.Root("username")) {
					t.Errorf("error %q is not reported against username", e.Summary())
				}
			}
		})
	}
}

func TestUserDataSourceRefusesTwoSpellingsOfOneName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/selectable", 200, `[{"id":"c2","name":"Alice"},{"id":"c1","name":"alice"}]`)
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("", "alice", "credentials"))
	requireErrorContaining(t, resp.Diagnostics, "apart from case")
}

func TestUserDataSourceExplainsRefusedAndMissingUsers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"another user with a non-admin key", 403,
			`{"message":"You are not allowed to view other users details","code":"FORBIDDEN"}`, "not an admin"},
		{"no such user", 404, `{"message":"User not found","code":"NOT_FOUND"}`, "no user with id u9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("GET /api/users/u9", tc.status, tc.body)
			d := &userDataSource{client: fake.client(t)}

			resp := runDataSourceRead(t, d, userLookup("u9", "", ""))
			requireErrorContaining(t, resp.Diagnostics, tc.want)
		})
	}
}

func TestUserDataSourceReportsANullHeaderWithAWarningOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	// GET /api/users/preferences is unrouted: a Homarr older than 2.3.0.
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("u1", "", ""))
	state := userDataSourceState(t, resp)

	requireWarning(t, resp.Diagnostics, "predates 2.3.0")
	if !state.Header.IsNull() {
		t.Errorf("header = %v, want null", state.Header)
	}
	if state.ColorScheme.ValueString() != "dark" {
		t.Errorf("color_scheme = %v, want the other preferences still read", state.ColorScheme)
	}
}

func TestUserDataSourceFailsOnAnyOtherHeaderError(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("u1", "", ""))
	requireErrorContaining(t, resp.Diagnostics, "boom")
}

// --- homarr_user: configuration ---------------------------------------------

// userDataSourcesProvider is the provider with the user data sources added,
// for the tests that validate a configuration the way Terraform does. A data
// source the provider registers already is not added twice.
type userDataSourcesProvider struct{ provider.Provider }

func (p userDataSourcesProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	typeName := func(newDataSource func() datasource.DataSource) string {
		var resp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "homarr"}, &resp)
		return resp.TypeName
	}
	constructors := p.Provider.DataSources(ctx)
	registered := map[string]bool{}
	for _, c := range constructors {
		registered[typeName(c)] = true
	}
	for _, c := range []func() datasource.DataSource{NewUserDataSource, NewCurrentUserDataSource, NewUserPreferencesDataSource} {
		if !registered[typeName(c)] {
			constructors = append(constructors, c)
		}
	}
	return constructors
}

func validateUserLookup(t *testing.T, config userDataSourceModel) []*tfprotov6.Diagnostic {
	t.Helper()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(userDataSourcesProvider{New("test")()})()
	if err != nil {
		t.Fatal(err)
	}
	s := dataSourceSchemaOf(t, &userDataSource{})
	dv, err := tfprotov6.NewDynamicValue(s.Type().TerraformType(ctx), dataSourceConfigOf(t, s, &config).Raw)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.ValidateDataResourceConfig(ctx, &tfprotov6.ValidateDataResourceConfigRequest{
		TypeName: "homarr_user",
		Config:   &dv,
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Diagnostics
}

func TestUserDataSourceConfigNeedsExactlyOneLookupKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  userDataSourceModel
		wantErr string
	}{
		{"id", userLookup("u1", "", ""), ""},
		{"username", userLookup("", "alice", ""), ""},
		{"username and provider", userLookup("", "alice", "oidc"), ""},
		{"neither", userLookup("", "", ""), "Exactly one of"},
		{"both", userLookup("u1", "alice", ""), "Exactly one of"},
		{"id and provider", userLookup("u1", "", "oidc"), "provider_name"},
		{"unknown provider", userLookup("", "alice", "github"), "credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateUserLookup(t, tc.config)
			switch {
			case tc.wantErr == "" && anyError(diags):
				t.Errorf("valid configuration rejected: %v", diags)
			case tc.wantErr != "" && !diagnosticsMention(diags, tc.wantErr):
				t.Errorf("no error mentioning %q in %v", tc.wantErr, diags)
			}
		})
	}

	empty := userLookup("", "", "")
	empty.Username = types.StringValue("")
	if diags := validateUserLookup(t, empty); !anyError(diags) {
		t.Error("an empty username was accepted")
	}
}

// --- homarr_current_user ------------------------------------------------------

func TestCurrentUserDataSourceReadsTheAPIKeysOwnUser(t *testing.T) {
	fake := newFakeHomarr()
	queries := preferencesQueries(fake, 200, userPreferencesJSON)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	d := &currentUserDataSource{client: fake.client(t)}

	state := userDataSourceState(t, runDataSourceRead(t, d, userLookup("", "", "")))

	if state.ID.ValueString() != "u1" || state.Username.ValueString() != "alice" {
		t.Errorf("id = %v, username = %v; want the key's own user", state.ID, state.Username)
	}
	if zoneLength(t, state.Header, "right") != 1 {
		t.Errorf("header = %v, want it read from the same preferences response", state.Header)
	}
	if got := queries(); !slices.Equal(got, []string{""}) {
		t.Errorf("preferences queries = %q, want a single request without a userId", got)
	}
}

func TestCurrentUserDataSourceNamesTheVersionOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr() // every request 404s, as on a Homarr before 2.3.0
	d := &currentUserDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("", "", ""))
	requireErrorContaining(t, resp.Diagnostics, "requires Homarr 2.3.0 or newer")
	if len(fake.callLog()) != 1 {
		t.Errorf("calls = %v, want only the preferences request", fake.callLog())
	}
}

// --- homarr_user_preferences --------------------------------------------------

func preferencesLookup(userID string) userPreferencesDataSourceModel {
	m := userPreferencesDataSourceModel{userPreferenceValues: userPreferenceValues{Header: types.ObjectNull(headerAttrTypes)}}
	if userID != "" {
		m.UserID = types.StringValue(userID)
	}
	return m
}

func userPreferencesDataSourceState(t *testing.T, resp datasource.ReadResponse) userPreferencesDataSourceModel {
	t.Helper()
	requireNoErrors(t, resp.Diagnostics)
	var state userPreferencesDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	return state
}

func TestUserPreferencesDataSourceReadsTheAPIKeysOwnByDefault(t *testing.T) {
	fake := newFakeHomarr()
	queries := preferencesQueries(fake, 200, strings.Replace(userPreferencesJSON, `"userId":"u1"`, `"userId":"me"`, 1))
	d := &userPreferencesDataSource{client: fake.client(t)}

	state := userPreferencesDataSourceState(t, runDataSourceRead(t, d, preferencesLookup("")))

	if state.ID.ValueString() != "me" || state.UserID.ValueString() != "me" {
		t.Errorf("id = %v, user_id = %v; want the key's own user reported", state.ID, state.UserID)
	}
	if got := queries(); !slices.Equal(got, []string{""}) {
		t.Errorf("preferences queries = %q, want no userId", got)
	}
}

func TestUserPreferencesDataSourceReadsANamedUser(t *testing.T) {
	fake := newFakeHomarr()
	queries := preferencesQueries(fake, 200, userPreferencesJSON)
	d := &userPreferencesDataSource{client: fake.client(t)}

	state := userPreferencesDataSourceState(t, runDataSourceRead(t, d, preferencesLookup("u1")))

	if got := queries(); !slices.Equal(got, []string{"userId=u1"}) {
		t.Errorf("preferences queries = %q, want the user named", got)
	}
	// A reference the user does not have is null here, never "".
	if !state.HomeBoardID.IsNull() || !state.DefaultSearchEngineID.IsNull() {
		t.Errorf("home_board_id = %v, default_search_engine_id = %v; want null", state.HomeBoardID, state.DefaultSearchEngineID)
	}
	if state.ColorScheme.ValueString() != "dark" || zoneLength(t, state.Header, "center") != 0 {
		t.Errorf("color_scheme = %v, header = %v; want dark and an empty center zone", state.ColorScheme, state.Header)
	}
}

func TestUserPreferencesDataSourceWithoutUserIDNamesTheVersionOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	d := &userPreferencesDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, preferencesLookup(""))
	requireErrorContaining(t, resp.Diagnostics, "requires Homarr 2.3.0 or newer")
	if len(fake.callLog()) != 1 {
		t.Errorf("calls = %v, want only the preferences request", fake.callLog())
	}
}

func TestUserPreferencesDataSourceFallsBackToTheUserOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"email":null`, `"homeBoardId":"b1"`, 1))
	d := &userPreferencesDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, preferencesLookup("u1"))
	state := userPreferencesDataSourceState(t, resp)

	requireWarning(t, resp.Diagnostics, "predates 2.3.0")
	if !state.Header.IsNull() {
		t.Errorf("header = %v, want null", state.Header)
	}
	if state.HomeBoardID.ValueString() != "b1" || state.ColorScheme.ValueString() != "dark" {
		t.Errorf("home_board_id = %v, color_scheme = %v; want them read from the user", state.HomeBoardID, state.ColorScheme)
	}
	if state.UserID.ValueString() != "u1" {
		t.Errorf("user_id = %v, want u1", state.UserID)
	}
}

func TestUserPreferencesDataSourceTellsMissingAndForbiddenUsersApart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"no such user", 404, `{"message":"User not found","code":"NOT_FOUND"}`, "no user with id u9"},
		{"another user with a non-admin key", 403,
			`{"message":"You are not allowed to view other users details","code":"FORBIDDEN"}`, "not an admin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			// Homarr answers 404 "User not found" for both, from the
			// preferences endpoint.
			fake.reply("GET /api/users/preferences", 404, `{"message":"User not found","code":"NOT_FOUND"}`)
			fake.reply("GET /api/users/u9", tc.status, tc.body)
			d := &userPreferencesDataSource{client: fake.client(t)}

			resp := runDataSourceRead(t, d, preferencesLookup("u9"))
			requireErrorContaining(t, resp.Diagnostics, tc.want)
			if resp.Diagnostics.WarningsCount() != 0 {
				t.Errorf("warned about the Homarr version: %v", resp.Diagnostics)
			}
		})
	}
}
