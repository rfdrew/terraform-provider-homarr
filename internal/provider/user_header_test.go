package provider

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

func homarrDefaultHeader() *client.HeaderPreferences {
	return &client.HeaderPreferences{
		Version:       client.HeaderPreferencesVersion,
		Visible:       true,
		SearchDisplay: "input",
		LogoDisplay:   "logoAndText",
		Zones: client.HeaderZones{
			Left:   []client.HeaderItem{{Type: "builtin", ID: "logo"}},
			Center: []client.HeaderItem{},
			Right: []client.HeaderItem{
				{Type: "board", BoardID: "b1"},
				{Type: "builtin", ID: "user"},
			},
		},
	}
}

// A layout read from Homarr and written back unchanged is the same layout,
// which is what lets an unchanged header plan no update.
func TestHeaderRoundTripsThroughTheAttribute(t *testing.T) {
	read := headerValueFromAPI(homarrDefaultHeader(), types.ObjectUnknown(headerAttrTypes))
	written, ok := headerPreferencesFromPlan(read)
	if !ok {
		t.Fatalf("a header read from Homarr could not be written back: %v", read)
	}
	if !reflect.DeepEqual(&written, homarrDefaultHeader()) {
		t.Fatalf("round trip = %+v, want %+v", written, homarrDefaultHeader())
	}
}

func TestHeaderKeepsTheSpellingOfEmptyZones(t *testing.T) {
	configured := headerConfig(map[string]attr.Value{
		"left":  headerZone(), // written as []
		"right": headerZone(headerItem("user", "")),
	}) // center left out
	read := headerValueFromAPI(&client.HeaderPreferences{
		Version: 3, Visible: true, SearchDisplay: "input", LogoDisplay: "logoAndText",
		Zones: client.HeaderZones{Left: []client.HeaderItem{}, Center: []client.HeaderItem{}, Right: []client.HeaderItem{{Type: "builtin", ID: "user"}}},
	}, configured)

	if !read.Equal(configured) {
		t.Fatalf("read back %v, want the configured %v", read, configured)
	}

	// Without a configuration to follow, an empty zone reads as left out.
	imported := headerValueFromAPI(homarrDefaultHeader(), types.ObjectNull(headerAttrTypes))
	if center := imported.Attributes()["center"].(types.List); !center.IsNull() {
		t.Fatalf("center = %v, want null for an empty zone nobody configured", center)
	}
}

func TestHeaderIsNullWhenHomarrReportsNone(t *testing.T) {
	if v := headerValueFromAPI(nil, types.ObjectUnknown(headerAttrTypes)); !v.IsNull() {
		t.Fatalf("header = %v, want null", v)
	}
}

// Homarr merges a header patch zone by zone, so every zone is sent: one left
// out of the configuration has to be emptied, not kept.
func TestPreferencesPatchSendsTheWholeHeader(t *testing.T) {
	prior := userTestModel().userPreferenceValues
	prior.Header = headerValueFromAPI(homarrDefaultHeader(), types.ObjectNull(headerAttrTypes))
	plan := prior
	plan.Header = headerConfig(map[string]attr.Value{"right": headerZone(headerItem("user", ""))})

	patch := preferencesPatch(plan, prior)
	raw, err := json.Marshal(patch["headerPreferences"])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":3,"visible":true,"searchDisplay":"input","logoDisplay":"logoAndText",` +
		`"zones":{"left":[],"center":[],"right":[{"type":"builtin","id":"user"}]}}`
	if string(raw) != want {
		t.Fatalf("headerPreferences =\n%s\nwant\n%s", raw, want)
	}
	if len(patch) != 1 {
		t.Fatalf("patch = %v, want only the header", patch)
	}
}

func TestPreferencesPatchLeavesAnUnchangedOrUnmanagedHeaderAlone(t *testing.T) {
	prior := userTestModel().userPreferenceValues
	prior.Header = headerValueFromAPI(homarrDefaultHeader(), types.ObjectNull(headerAttrTypes))

	for name, header := range map[string]types.Object{
		"unchanged":     prior.Header,
		"not set":       types.ObjectUnknown(headerAttrTypes),
		"board unknown": headerConfig(map[string]attr.Value{"right": headerZone(types.ObjectUnknown(headerItemAttrTypes))}),
	} {
		plan := prior
		plan.Header = header
		if patch := preferencesPatch(plan, prior); len(patch) != 0 {
			t.Errorf("%s: patch = %v, want none", name, patch)
		}
	}
}

// --- homarr_user_preferences without user_id -----------------------------------

func userPreferencesCreatePlan(userID types.String) userPreferencesModel {
	m := userPreferencesModel{ID: types.StringUnknown(), UserID: userID, Image: types.StringUnknown()}
	m.HomeBoardID, m.MobileHomeBoardID = types.StringUnknown(), types.StringUnknown()
	m.DefaultSearchEngineID, m.ByteUnitSystem = types.StringUnknown(), types.StringUnknown()
	m.ColorScheme = types.StringValue("light")
	m.FirstDayOfWeek = types.Int64Unknown()
	m.PingIconsEnabled, m.EnableRightClickOnWidgets = types.BoolUnknown(), types.BoolUnknown()
	m.OpenSearchInNewTab, m.DdgBangs = types.BoolUnknown(), types.BoolUnknown()
	m.Header = types.ObjectUnknown(headerAttrTypes)
	return m
}

func runUserPreferencesCreate(t *testing.T, r *userPreferencesResource, plan userPreferencesModel) resource.CreateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: nullState(s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, s, &plan), Config: configOf(t, s, &plan)}, &resp)
	return resp
}

func TestUserPreferencesWithoutUserIDTargetTheAPIKeysUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 200,
		`{"userId":"me","colorScheme":"light","byteUnitSystem":"binary","firstDayOfWeek":1,"headerPreferences":`+
			`{"version":3,"visible":true,"searchDisplay":"input","logoDisplay":"logoAndText","zones":`+
			`{"left":[],"center":[],"right":[{"type":"builtin","id":"user"}]}}}`)
	fake.reply("GET /api/users/me", 200, `{"id":"me","name":"admin","provider":"credentials"}`)
	r := &userPreferencesResource{client: fake.client(t)}

	resp := runUserPreferencesCreate(t, r, userPreferencesCreatePlan(types.StringUnknown()))
	requireNoErrors(t, resp.Diagnostics)

	if _, present := fake.body(t, "PATCH /api/users/preferences")["userId"]; present {
		t.Fatal("sent a userId although user_id was left out")
	}
	var state userPreferencesModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if state.UserID.ValueString() != "me" || state.ID.ValueString() != "me" {
		t.Fatalf("user_id = %v, id = %v; want the API key's user from the response", state.UserID, state.ID)
	}
	if state.Header.IsNull() || state.Header.IsUnknown() {
		t.Fatalf("header = %v, want it read from the response", state.Header)
	}
}

func TestUserPreferencesWithoutUserIDNameTheVersionOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr() // PATCH /api/users/preferences is unrouted
	r := &userPreferencesResource{client: fake.client(t)}

	resp := runUserPreferencesCreate(t, r, userPreferencesCreatePlan(types.StringUnknown()))
	requireErrorContaining(t, resp.Diagnostics, "requires Homarr 2.3.0 or newer")
	for _, d := range resp.Diagnostics.Errors() {
		if d.Summary() == "User not found" {
			t.Fatalf("a missing endpoint was reported as a missing user: %v", resp.Diagnostics)
		}
	}
}

func TestUserPreferencesUserIDStaysPutUnlessConfigured(t *testing.T) {
	model := userPreferencesCreatePlan(types.StringValue("u1"))
	model.ID = types.StringValue("u1")
	model.Header = types.ObjectNull(headerAttrTypes)
	r := &userPreferencesResource{}

	// Left out: the id resolved at creation is kept, with no replacement.
	planned, replace := runStringModifiers(t, r, &model, "user_id",
		types.StringNull(), types.StringValue("u1"), types.StringUnknown())
	if replace || planned.ValueString() != "u1" {
		t.Errorf("omitted user_id: plan = %v, replace = %v; want u1 kept in place", planned, replace)
	}

	// Configured to the same user: nothing to do.
	if _, replace := runStringModifiers(t, r, &model, "user_id",
		types.StringValue("u1"), types.StringValue("u1"), types.StringValue("u1")); replace {
		t.Error("the same configured user_id plans a replacement")
	}

	// Configured to another user: a new resource.
	if _, replace := runStringModifiers(t, r, &model, "user_id",
		types.StringValue("u2"), types.StringValue("u1"), types.StringValue("u2")); !replace {
		t.Error("a different user_id does not plan a replacement")
	}
}

// --- homarr_users ------------------------------------------------------------------

func TestUsersDataSourceSortsByNameThenID(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users", 200, `[{"id":"z","name":"bob"},{"id":"n1","name":null},{"id":"b","name":"alice"},`+
		`{"id":"a","name":"alice"},{"id":"c","name":"carol","email":"c@example.com"}]`)
	fake.reply("GET /api/users/selectable", 200, `[]`)
	d := &usersDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, usersDataSourceModel{})
	requireNoErrors(t, resp.Diagnostics)

	var state usersDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	var ids []string
	for _, u := range state.Users {
		ids = append(ids, u.ID.ValueString())
	}
	// alice (a, then b), bob, carol, and the unnamed user last.
	if want := []string{"a", "b", "z", "c", "n1"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
	last := state.Users[len(state.Users)-1]
	if !last.Name.IsNull() {
		t.Fatalf("name = %v, want null for a user Homarr stores without one", last.Name)
	}
}
