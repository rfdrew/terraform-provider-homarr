package provider

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// These tests drive homarr_user's CRUD methods against the in-process fake
// Homarr from fallbacks_test.go, for the behaviour the acceptance suite cannot
// set up: failures halfway through a create, old releases, other auth
// providers, and the API key's own account.

// userDetailJSON is GET /api/users/u1 for the user userTestModel describes. Its
// headerPreferences is the raw, legacy text that endpoint reports, which the
// provider must not use.
const userDetailJSON = `{"id":"u1","name":"alice","email":null,"provider":"credentials","colorScheme":"dark",` +
	`"byteUnitSystem":"binary","firstDayOfWeek":1,"pingIconsEnabled":false,"enableRightClickOnWidgets":true,` +
	`"openSearchInNewTab":true,"ddgBangs":true,"headerPreferences":"{\"version\":1,\"visible\":true,\"items\":[\"user\"]}"}`

// userPreferencesJSON is GET or PATCH /api/users/preferences for the same user.
const userPreferencesJSON = `{"userId":"u1","colorScheme":"dark","byteUnitSystem":"binary","firstDayOfWeek":1,` +
	`"pingIconsEnabled":false,"enableRightClickOnWidgets":true,"homeBoardId":null,"mobileHomeBoardId":null,` +
	`"defaultSearchEngineId":null,"openSearchInNewTab":true,"ddgBangs":true,"headerPreferences":{"version":3,` +
	`"visible":true,"searchDisplay":"input","logoDisplay":"logoAndText","zones":{"left":[{"type":"builtin","id":"logo"}],` +
	`"center":[],"right":[{"type":"builtin","id":"user"}]}}}`

// userTestModel is a homarr_user as state holds it after a read.
func userTestModel() userModel {
	return userModel{
		ID:                types.StringValue("u1"),
		Username:          types.StringValue("alice"),
		Password:          types.StringValue("Correct-Horse-1"),
		PasswordWO:        types.StringNull(),
		PasswordWOVersion: types.Int64Null(),
		Email:             types.StringNull(),
		GroupIDs:          types.ListNull(types.StringType),
		Provider:          types.StringValue("credentials"),
		userPreferenceValues: userPreferenceValues{
			HomeBoardID:               types.StringNull(),
			MobileHomeBoardID:         types.StringNull(),
			DefaultSearchEngineID:     types.StringNull(),
			ColorScheme:               types.StringValue("dark"),
			ByteUnitSystem:            types.StringValue("binary"),
			FirstDayOfWeek:            types.Int64Value(1),
			PingIconsEnabled:          types.BoolValue(false),
			EnableRightClickOnWidgets: types.BoolValue(true),
			OpenSearchInNewTab:        types.BoolValue(true),
			DdgBangs:                  types.BoolValue(true),
			Header:                    types.ObjectNull(headerAttrTypes),
		},
	}
}

// userCreatePlan is the plan for creating userTestModel with no preference
// configured: everything Homarr decides is unknown.
func userCreatePlan() userModel {
	m := userTestModel()
	m.ID, m.Provider, m.Image = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	m.ColorScheme, m.ByteUnitSystem = types.StringUnknown(), types.StringUnknown()
	m.FirstDayOfWeek = types.Int64Unknown()
	m.PingIconsEnabled, m.EnableRightClickOnWidgets = types.BoolUnknown(), types.BoolUnknown()
	m.OpenSearchInNewTab, m.DdgBangs = types.BoolUnknown(), types.BoolUnknown()
	m.Header = types.ObjectUnknown(headerAttrTypes)
	return m
}

func configOf(t *testing.T, s schema.Schema, model any) tfsdk.Config {
	t.Helper()
	return tfsdk.Config{Schema: s, Raw: stateOf(t, s, model).Raw}
}

func nullState(s schema.Schema) tfsdk.State {
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

// replySequence answers successive requests to route with bodies in turn,
// repeating the last one, so that GET /api/users can change around a create.
func (f *fakeHomarr) replySequence(route string, bodies ...string) {
	var mu sync.Mutex
	next := 0
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := bodies[next]
		if next < len(bodies)-1 {
			next++
		}
		mu.Unlock()
		_, _ = io.WriteString(w, body)
	})
}

// callLog returns the requests made so far, in order.
func (f *fakeHomarr) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func runUserCreate(t *testing.T, r *userResource, plan, config userModel) resource.CreateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: nullState(s)}
	r.Create(context.Background(), resource.CreateRequest{
		Plan:   planOf(t, s, &plan),
		Config: configOf(t, s, &config),
	}, &resp)
	return resp
}

func runUserUpdate(t *testing.T, r *userResource, prior, plan, config userModel) resource.UpdateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	planned := planOf(t, s, &plan)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: planned.Raw}}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:   planned,
		State:  stateOf(t, s, &prior),
		Config: configOf(t, s, &config),
	}, &resp)
	return resp
}

func stateID(t *testing.T, state tfsdk.State) string {
	t.Helper()
	var id types.String
	requireNoErrors(t, state.GetAttribute(context.Background(), path.Root("id"), &id))
	return id.ValueString()
}

// --- in-place profile updates ----------------------------------------------

func TestUserUpdateChangesTheProfileThenThePasswordThenThePreferences(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PUT /api/users/profile", 200, ``)
	fake.reply("PATCH /api/users/u1/changePassword", 200, ``)
	fake.reply("PATCH /api/users/preferences", 200, userPreferencesJSON)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"name":"alice"`, `"name":"bob"`, 1))
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	plan := userTestModel()
	plan.Username = types.StringValue("bob")
	plan.Password = types.StringValue("New-Password-1")
	plan.ColorScheme = types.StringValue("light")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireNoErrors(t, resp.Diagnostics)

	var writes []string
	for _, call := range fake.callLog() {
		if !strings.HasPrefix(call, "GET ") {
			writes = append(writes, call)
		}
	}
	want := []string{"PUT /api/users/profile", "PATCH /api/users/u1/changePassword", "PATCH /api/users/preferences"}
	if !slices.Equal(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}

	body := fake.body(t, "PUT /api/users/profile")
	if body["id"] != "u1" || body["name"] != "bob" {
		t.Errorf("profile body = %v, want id u1 and name bob", body)
	}
	if _, present := body["email"]; present {
		t.Errorf("profile body = %v, want no email for a user without one", body)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if state.Username.ValueString() != "bob" {
		t.Errorf("username = %v, want bob read back", state.Username)
	}
}

// Homarr requires the name even when only the email changes.
func TestUserUpdateChangesTheEmailInPlace(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PUT /api/users/profile", 200, ``)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"email":null`, `"email":"new@example.com"`, 1))
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	prior.Email = types.StringValue("old@example.com")
	plan := prior
	plan.Email = types.StringValue("new@example.com")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PUT /api/users/profile")
	if body["name"] != "alice" || body["email"] != "new@example.com" {
		t.Errorf("profile body = %v, want the current name and the new email", body)
	}
	if fake.called("PATCH /api/users/u1/changePassword") || fake.called("PATCH /api/users/preferences") {
		t.Error("an email change touched the password or the preferences")
	}
}

func TestUserRenameToATakenNameIsReportedOnUsername(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PUT /api/users/profile", 409, `{"message":"Username already taken","code":"CONFLICT"}`)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	plan := prior
	plan.Username = types.StringValue("bob")
	plan.Password = types.StringValue("New-Password-1")

	resp := runUserUpdate(t, r, prior, plan, plan)
	if resp.Diagnostics.ErrorsCount() != 1 {
		t.Fatalf("want exactly one error, got %v", resp.Diagnostics)
	}
	withPath, ok := resp.Diagnostics.Errors()[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(path.Root("username")) {
		t.Fatalf("error is not attached to username: %v", resp.Diagnostics)
	}
	if fake.called("PATCH /api/users/u1/changePassword") {
		t.Error("the password was changed although the rename before it failed")
	}
}

// runStringModifiers applies an attribute's plan modifiers in order, the way
// the framework does, and returns the resulting plan value and whether a
// replacement was asked for.
func runStringModifiers(t *testing.T, r resource.Resource, model any, name string, config, prior, planned types.String) (types.String, bool) {
	t.Helper()
	s := resourceSchema(t, r)
	attribute, ok := s.Attributes[name].(schema.StringAttribute)
	if !ok {
		t.Fatalf("%s is not a string attribute", name)
	}
	req := planmodifier.StringRequest{
		Path:        path.Root(name),
		Config:      configOf(t, s, model),
		Plan:        planOf(t, s, model),
		State:       stateOf(t, s, model),
		ConfigValue: config,
		StateValue:  prior,
		PlanValue:   planned,
	}
	resp := &planmodifier.StringResponse{PlanValue: planned}
	for _, m := range attribute.PlanModifiers {
		m.PlanModifyString(context.Background(), req, resp)
		req.PlanValue = resp.PlanValue
	}
	return resp.PlanValue, resp.RequiresReplace
}

func TestUserRenameDoesNotReplace(t *testing.T) {
	model := userTestModel()
	_, replace := runStringModifiers(t, &userResource{}, &model, "username",
		types.StringValue("bob"), types.StringValue("alice"), types.StringValue("bob"))
	if replace {
		t.Fatal("a rename plans a new user")
	}
}

// Homarr can set or change an email but never clear one.
func TestUserEmailOnlyReplacesWhenRemoved(t *testing.T) {
	model := userTestModel()
	for name, tc := range map[string]struct {
		prior, planned types.String
		want           bool
	}{
		"removed":                   {types.StringValue("a@example.com"), types.StringNull(), true},
		"added":                     {types.StringNull(), types.StringValue("a@example.com"), false},
		"changed":                   {types.StringValue("a@example.com"), types.StringValue("b@example.com"), false},
		"an empty one left as none": {types.StringValue(""), types.StringNull(), false},
	} {
		_, replace := runStringModifiers(t, &userResource{}, &model, "email", tc.planned, tc.prior, tc.planned)
		if replace != tc.want {
			t.Errorf("%s: replace = %v, want %v", name, replace, tc.want)
		}
	}
}

func TestUserReadsAnEmptyEmailAsNone(t *testing.T) {
	m := userTestModel()
	empty := ""
	m.fromAPI(&client.UserDetail{ID: "u1", Email: &empty})
	if !m.Email.IsNull() {
		t.Fatalf("email = %v, want null for an email stored as \"\"", m.Email)
	}
}

// --- group_ids ---------------------------------------------------------------

func groupList(values ...string) types.List {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elements)
}

func TestGroupIDsReplaceOnlyWhenTheMembershipChanges(t *testing.T) {
	model := userTestModel()
	s := resourceSchema(t, &userResource{})
	attribute, ok := s.Attributes["group_ids"].(schema.ListAttribute)
	if !ok {
		t.Fatal("group_ids is not a list attribute")
	}

	for name, tc := range map[string]struct {
		prior, planned types.List
		want           bool
	}{
		"reordered":        {groupList("g1", "g2"), groupList("g2", "g1"), false},
		"null to empty":    {types.ListNull(types.StringType), groupList(), false},
		"a group added":    {groupList("g1"), groupList("g1", "g2"), true},
		"a group removed":  {groupList("g1", "g2"), groupList("g1"), true},
		"a group replaced": {groupList("g1"), groupList("g2"), true},
		"not known yet":    {groupList("g1"), types.ListUnknown(types.StringType), true},
	} {
		req := planmodifier.ListRequest{
			Path:        path.Root("group_ids"),
			Config:      configOf(t, s, &model),
			Plan:        planOf(t, s, &model),
			State:       stateOf(t, s, &model),
			ConfigValue: tc.planned,
			StateValue:  tc.prior,
			PlanValue:   tc.planned,
		}
		resp := &planmodifier.ListResponse{PlanValue: tc.planned}
		for _, m := range attribute.PlanModifiers {
			m.PlanModifyList(context.Background(), req, resp)
		}
		if resp.RequiresReplace != tc.want {
			t.Errorf("%s: replace = %v, want %v", name, resp.RequiresReplace, tc.want)
		}
	}
}

// --- creation that fails halfway ---------------------------------------------

func TestUserCreateKeepsTheIDWhenTheReadBackFails(t *testing.T) {
	fake := newFakeHomarr()
	fake.replySequence("GET /api/users", `[]`, `[{"id":"u1","name":"alice"}]`)
	fake.reply("POST /api/users", 200, ``)
	fake.reply("GET /api/users/u1", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	resp := runUserCreate(t, r, plan, plan)

	requireErrorContaining(t, resp.Diagnostics, "boom")
	requireErrorContaining(t, resp.Diagnostics, "tainted")
	if id := stateID(t, resp.State); id != "u1" {
		t.Fatalf("state id = %q, want u1 recorded so that the user is not orphaned", id)
	}
}

func TestUserCreateRecordsTheAccountAFailedPostLeftBehind(t *testing.T) {
	fake := newFakeHomarr()
	fake.replySequence("GET /api/users", `[]`, `[{"id":"u1","name":"alice"}]`)
	fake.reply("POST /api/users", 500, `{"message":"FOREIGN KEY constraint failed","code":"INTERNAL_SERVER_ERROR"}`)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	plan.GroupIDs = groupList("no-such-group")
	resp := runUserCreate(t, r, plan, plan)

	requireErrorContaining(t, resp.Diagnostics, "FOREIGN KEY")
	requireErrorContaining(t, resp.Diagnostics, "tainted")
	if id := stateID(t, resp.State); id != "u1" {
		t.Fatalf("state id = %q, want the half-created user recorded", id)
	}
	if fake.called("GET /api/users/u1") {
		t.Error("the create carried on after Homarr failed it")
	}
}

func TestUserCreateLeavesNoStateWhenNothingWasCreated(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[]`)
	fake.reply("POST /api/users", 400, `{"message":"Invalid input","code":"BAD_REQUEST"}`)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	resp := runUserCreate(t, r, plan, plan)

	requireErrorContaining(t, resp.Diagnostics, "Invalid input")
	if !resp.State.Raw.IsNull() {
		t.Fatal("state was written for a user Homarr did not create")
	}
}

// An account an earlier apply created but never recorded holds the name; the
// error points at the import that adopts it.
func TestUserCreateOnATakenNamePointsAtTheImportByUsername(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"u1","name":"alice"}]`)
	fake.reply("POST /api/users", 409, `{"message":"Username already taken","code":"CONFLICT"}`)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	resp := runUserCreate(t, r, plan, plan)

	requireAttributeError(t, resp.Diagnostics, "username")
	requireErrorContaining(t, resp.Diagnostics, `"username=alice"`)
}

// --- password_wo ---------------------------------------------------------------

func createdUserFake(t *testing.T) *fakeHomarr {
	t.Helper()
	fake := newFakeHomarr()
	fake.replySequence("GET /api/users", `[]`, `[{"id":"u1","name":"alice"}]`)
	fake.reply("POST /api/users", 200, ``)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	return fake
}

func TestUserCreateSendsTheWriteOnlyPassword(t *testing.T) {
	fake := createdUserFake(t)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	plan.Password = types.StringNull()
	plan.PasswordWOVersion = types.Int64Value(1)
	config := plan
	config.PasswordWO = types.StringValue("Write-Only-Pass-1")

	resp := runUserCreate(t, r, plan, config)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "POST /api/users")
	if body["password"] != "Write-Only-Pass-1" || body["confirmPassword"] != "Write-Only-Pass-1" {
		t.Fatalf("POST /api/users body = %v, want the write-only password", body)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.PasswordWO.IsNull() || !state.Password.IsNull() {
		t.Fatalf("state holds a password: password = %v, password_wo = %v", state.Password, state.PasswordWO)
	}
}

func TestUserUpdateSendsTheWriteOnlyPasswordOnlyWhenTheVersionChanges(t *testing.T) {
	for name, tc := range map[string]struct {
		priorVersion, plannedVersion types.Int64
		want                         bool
	}{
		"version bumped":                 {types.Int64Value(1), types.Int64Value(2), true},
		"version unchanged":              {types.Int64Value(1), types.Int64Value(1), false},
		"switched from password with v1": {types.Int64Null(), types.Int64Value(1), true},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("PATCH /api/users/u1/changePassword", 200, ``)
			fake.reply("GET /api/users/u1", 200, userDetailJSON)
			fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
			r := &userResource{client: fake.client(t)}

			prior := userTestModel()
			prior.PasswordWOVersion = tc.priorVersion
			if !tc.priorVersion.IsNull() {
				prior.Password = types.StringNull()
			}
			plan := prior
			plan.Password = types.StringNull()
			plan.PasswordWOVersion = tc.plannedVersion
			config := plan
			config.PasswordWO = types.StringValue("Rotated-Pass-2")

			resp := runUserUpdate(t, r, prior, plan, config)
			requireNoErrors(t, resp.Diagnostics)

			if got := fake.called("PATCH /api/users/u1/changePassword"); got != tc.want {
				t.Fatalf("password changed = %v, want %v", got, tc.want)
			}
			if tc.want {
				if body := fake.body(t, "PATCH /api/users/u1/changePassword"); body["password"] != "Rotated-Pass-2" {
					t.Fatalf("changePassword body = %v, want the write-only password", body)
				}
			}
		})
	}
}

// Homarr checks the previous password only for the API key's own user. When
// state cannot supply it, the configured password is the right previous one
// once it was set in the UI.
func TestUserPasswordChangeRetriesWithTheNewPassword(t *testing.T) {
	fake := newFakeHomarr()
	var mu sync.Mutex
	var previous []string
	fake.handle("PATCH /api/users/u1/changePassword", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(string(raw), `"previousPassword":"New-Password-1"`) {
			previous = append(previous, "new")
			return
		}
		previous = append(previous, "state")
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"message":"Invalid password","code":"FORBIDDEN"}`)
	})
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	plan := prior
	plan.Password = types.StringValue("New-Password-1")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireNoErrors(t, resp.Diagnostics)
	if !slices.Equal(previous, []string{"state", "new"}) {
		t.Fatalf("previous passwords tried = %v, want the one in state, then the new one", previous)
	}
}

func TestUserPasswordChangeExplainsTheAPIKeysOwnUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/u1/changePassword", 403, `{"message":"Invalid password","code":"FORBIDDEN"}`)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	plan := prior
	plan.Password = types.StringValue("New-Password-1")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireErrorContaining(t, resp.Diagnostics, "owns the provider's API key")
}

// --- reading -------------------------------------------------------------------

func runUserRead(t *testing.T, r *userResource, prior userModel) resource.ReadResponse {
	t.Helper()
	state := stateOf(t, resourceSchema(t, r), &prior)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	return resp
}

func TestUserReadsTheHeaderFromThePreferencesEndpoint(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	resp := runUserRead(t, r, userTestModel())
	requireNoErrors(t, resp.Diagnostics)

	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	header, ok := headerPreferencesFromPlan(state.Header)
	if !ok {
		t.Fatalf("header = %v, want the layout read back", state.Header)
	}
	if len(header.Zones.Left) != 1 || header.Zones.Left[0].ID != "logo" || len(header.Zones.Right) != 1 || header.Zones.Right[0].ID != "user" {
		t.Fatalf("header = %+v, want the parsed layout from /api/users/preferences", header)
	}
}

func TestUserReadKeepsANullHeaderQuietlyOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr() // GET /api/users/preferences is unrouted: older than 2.3.0
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	r := &userResource{client: fake.client(t)}

	resp := runUserRead(t, r, userTestModel())
	requireNoErrors(t, resp.Diagnostics)
	if resp.Diagnostics.WarningsCount() != 0 {
		t.Fatalf("warned about a header nobody manages: %v", resp.Diagnostics)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.Header.IsNull() || state.ColorScheme.ValueString() != "dark" {
		t.Fatalf("header = %v, color_scheme = %v; want null and the rest still read", state.Header, state.ColorScheme)
	}
}

func TestUserCreateLeavesTheHeaderNullWithAWarningOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.replySequence("GET /api/users", `[]`, `[{"id":"u1","name":"alice"}]`)
	fake.reply("POST /api/users", 200, ``)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	resp := runUserCreate(t, r, plan, plan)
	requireNoErrors(t, resp.Diagnostics)
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("want one warning about the header, got %v", resp.Diagnostics)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.Header.IsNull() {
		t.Fatalf("header = %v, want null", state.Header)
	}
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatal("state still holds unknown values")
	}
}

// An LDAP or OIDC account imported before the provider refused them is still
// read, so that a `removed` block or a destroy, which refresh it first, work.
func TestUserReadWarnsAboutAnLDAPOrOIDCAccount(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"provider":"credentials"`, `"provider":"oidc"`, 1))
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	resp := runUserRead(t, r, userTestModel())
	requireNoErrors(t, resp.Diagnostics)
	requireWarning(t, resp.Diagnostics, "not a credentials account")
	requireWarning(t, resp.Diagnostics, "`removed` block")

	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if state.Provider.ValueString() != "oidc" {
		t.Fatalf("provider_name = %v, want oidc read back", state.Provider)
	}
}

// --- import ----------------------------------------------------------------------

func runUserImport(t *testing.T, r *userResource, id string) resource.ImportStateResponse {
	t.Helper()
	resp := resource.ImportStateResponse{State: nullState(resourceSchema(t, r))}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
	return resp
}

func TestUserImportByUsernamePicksTheCredentialsAccount(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"ldap-1","name":"alice"},{"id":"bob-1","name":"bob"},{"id":"cred-1","name":"alice"}]`)
	fake.reply("GET /api/users/ldap-1", 200, `{"id":"ldap-1","name":"alice","provider":"ldap"}`)
	fake.reply("GET /api/users/cred-1", 200, `{"id":"cred-1","name":"alice","provider":"credentials"}`)
	r := &userResource{client: fake.client(t)}

	resp := runUserImport(t, r, "username=alice")
	requireNoErrors(t, resp.Diagnostics)
	if id := stateID(t, resp.State); id != "cred-1" {
		t.Fatalf("imported id = %q, want the credentials account cred-1", id)
	}
	if fake.called("GET /api/users/bob-1") {
		t.Error("read a user of another name")
	}
}

func TestUserImportByUsernameRefusesExternalAccounts(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"ldap-1","name":"alice"}]`)
	fake.reply("GET /api/users/ldap-1", 200, `{"id":"ldap-1","name":"alice","provider":"ldap"}`)
	r := &userResource{client: fake.client(t)}

	resp := runUserImport(t, r, "username=alice")
	requireErrorContaining(t, resp.Diagnostics, "ldap")
	requireErrorContaining(t, resp.Diagnostics, "homarr_user_preferences")
}

func TestUserImportByUsernameReportsAnUnknownName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"u1","name":null},{"id":"u2","name":"bob"}]`)
	r := &userResource{client: fake.client(t)}

	requireErrorContaining(t, runUserImport(t, r, "username=alice").Diagnostics, `No Homarr user is named "alice"`)
	requireErrorContaining(t, runUserImport(t, r, "username=").Diagnostics, "must not be empty")
}

func TestUserImportByIDChecksTheAuthProvider(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"a credentials account", 200, userDetailJSON, ""},
		{"an OIDC account", 200, strings.Replace(userDetailJSON, `"provider":"credentials"`, `"provider":"oidc"`, 1),
			"signs in through oidc"},
		// Read, which runs next, reports this one.
		{"a failed read", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("GET /api/users/u1", tc.status, tc.body)
			r := &userResource{client: fake.client(t)}

			resp := runUserImport(t, r, "u1")
			if tc.wantErr != "" {
				requireErrorContaining(t, resp.Diagnostics, tc.wantErr)
				requireErrorContaining(t, resp.Diagnostics, "homarr_user_preferences")
				return
			}
			requireNoErrors(t, resp.Diagnostics)
			if id := stateID(t, resp.State); id != "u1" {
				t.Fatalf("imported id = %q", id)
			}
		})
	}
}

// --- the API key's own user ---------------------------------------------------

func runUserDelete(t *testing.T, r *userResource) resource.DeleteResponse {
	t.Helper()
	model := userTestModel()
	state := stateOf(t, resourceSchema(t, r), &model)
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	return resp
}

func TestUserDeleteRefusesTheAPIKeysOwnUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 200, `{"userId":"u1"}`)
	fake.reply("DELETE /api/users/u1", 200, ``)
	r := &userResource{client: fake.client(t)}

	requireErrorContaining(t, runUserDelete(t, r).Diagnostics, "owns the API key")
	if fake.called("DELETE /api/users/u1") {
		t.Fatal("deleted the user that owns the provider's API key")
	}
}

func TestUserDeleteRemovesAnotherUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 200, `{"userId":"admin"}`)
	fake.reply("DELETE /api/users/u1", 200, ``)
	r := &userResource{client: fake.client(t)}

	requireNoErrors(t, runUserDelete(t, r).Diagnostics)
	if !fake.called("DELETE /api/users/u1") {
		t.Fatal("the user was not deleted")
	}
}

func TestUserDeleteSkipsTheOwnerCheckOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr() // GET /api/users/preferences is unrouted
	fake.reply("DELETE /api/users/u1", 200, ``)
	r := &userResource{client: fake.client(t)}

	requireNoErrors(t, runUserDelete(t, r).Diagnostics)
	if !fake.called("DELETE /api/users/u1") {
		t.Fatal("the user was not deleted")
	}
}

// On Homarr 2.0-2.2 a search engine picked in the UI reads back into state,
// and since homarr_user owns default_search_engine_id outright, the next plan
// clears it. That must work there too, rather than fail every apply, including
// those for unrelated changes.
func TestUserUpdateClearsAUISearchEngineOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr() // the /api/users/preferences pair is unrouted
	fake.reply("PUT /api/users/profile", 200, ``)
	fake.reply("PATCH /api/users/search-preferences", 200, ``)
	renamed := strings.Replace(userDetailJSON, `"name":"alice"`, `"name":"bob"`, 1)
	fake.replySequence("GET /api/users/u1",
		strings.Replace(renamed, `"provider"`, `"defaultSearchEngineId":"se1","provider"`, 1),
		renamed)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	prior.DefaultSearchEngineID = types.StringValue("se1")
	plan := userTestModel()
	plan.Username = types.StringValue("bob")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PATCH /api/users/search-preferences")
	if engine, present := body["defaultSearchEngineId"]; !present || engine != nil {
		t.Fatalf("search-preferences body = %v, want an explicit null search engine", body)
	}
	if body["openInNewTab"] != true || body["ddgBangsEnabled"] != true {
		t.Fatalf("search-preferences body = %v, want the other two search preferences kept", body)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.DefaultSearchEngineID.IsNull() || state.Username.ValueString() != "bob" {
		t.Fatalf("state has default_search_engine_id %v and username %v, want null and bob",
			state.DefaultSearchEngineID, state.Username)
	}
}

func TestUserDeleteStopsWhenTheOwnerCheckFails(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("DELETE /api/users/u1", 200, ``)
	r := &userResource{client: fake.client(t)}

	requireErrorContaining(t, runUserDelete(t, r).Diagnostics, "boom")
	if fake.called("DELETE /api/users/u1") {
		t.Fatal("deleted the user without knowing whether it owns the API key")
	}
}

func runUserDestroyPlan(t *testing.T, r *userResource) resource.ModifyPlanResponse {
	t.Helper()
	s := resourceSchema(t, r)
	model := userTestModel()
	destroy := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	resp := resource.ModifyPlanResponse{Plan: destroy}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		State:  stateOf(t, s, &model),
		Plan:   destroy,
		Config: tfsdk.Config{Schema: s, Raw: destroy.Raw},
	}, &resp)
	return resp
}

func TestUserPlanRefusesToDestroyTheAPIKeysOwnUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 200, `{"userId":"u1"}`)
	r := &userResource{client: fake.client(t)}

	requireErrorContaining(t, runUserDestroyPlan(t, r).Diagnostics, "owns the API key")
}

// At plan time the check is a courtesy; Delete makes the call that counts.
func TestUserPlanLeavesAnUnansweredOwnerCheckToDelete(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &userResource{client: fake.client(t)}

	requireNoErrors(t, runUserDestroyPlan(t, r).Diagnostics)
}

func TestUserPreferencesReportARefusedHeaderShortcutAgainstTheAttribute(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 400,
		`{"message":"One or more board shortcuts are unavailable to this user","code":"BAD_REQUEST"}`)
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", map[string]any{"headerPreferences": map[string]any{}}, &diags)
	if diags.ErrorsCount() != 1 {
		t.Fatalf("want exactly one error, got %v", diags)
	}
	withPath, ok := diags.Errors()[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(path.Root("header")) {
		t.Fatalf("error is not attached to header: %v", diags)
	}
}
