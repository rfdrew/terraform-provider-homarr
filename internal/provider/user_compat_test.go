package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// These tests cover Homarr releases before 2.3.0 as a key that is not an
// admin's sees them, and state written before the header attribute existed.

// legacyPreferencesRefusal is what a Homarr before 2.3.0 answers such a key for
// GET /api/users/preferences: with no such route, the request lands on
// GET /api/users/{userId}, which refuses to show "another" user.
const legacyPreferencesRefusal = `{"message":"You are not allowed to view other users details","code":"FORBIDDEN"}`

func TestCurrentUserDataSourceNamesTheVersionForANonAdminKeyOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 403, legacyPreferencesRefusal)
	d := &currentUserDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("", "", ""))
	requireErrorContaining(t, resp.Diagnostics, "requires Homarr 2.3.0 or newer")
}

func TestUserDataSourceReadsTheKeysOwnUserOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/selectable", 200, `[{"id":"u1","name":"alice"}]`)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 403, legacyPreferencesRefusal)
	d := &userDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, userLookup("", "alice", ""))
	state := userDataSourceState(t, resp)

	requireWarning(t, resp.Diagnostics, "predates 2.3.0")
	if !state.Header.IsNull() || state.ColorScheme.ValueString() != "dark" {
		t.Errorf("header = %v, color_scheme = %v; want null and the rest still read", state.Header, state.ColorScheme)
	}
}

func TestUserPreferencesDataSourceFallsBackForANonAdminKeyOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 403, legacyPreferencesRefusal)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	d := &userPreferencesDataSource{client: fake.client(t)}

	resp := runDataSourceRead(t, d, preferencesLookup("u1"))
	state := userPreferencesDataSourceState(t, resp)

	requireWarning(t, resp.Diagnostics, "predates 2.3.0")
	if state.ColorScheme.ValueString() != "dark" || !state.Header.IsNull() {
		t.Errorf("color_scheme = %v, header = %v; want them read from the user", state.ColorScheme, state.Header)
	}
}

func TestUserPreferencesResourceNamesTheVersionForANonAdminKeyOnOldHomarr(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 403, legacyPreferencesRefusal)
	r := &userPreferencesResource{client: fake.client(t)}

	// Nothing configured: the create only reads.
	plan := userPreferencesCreatePlan(types.StringUnknown())
	plan.ColorScheme = types.StringUnknown()
	resp := runUserPreferencesCreate(t, r, plan)
	requireErrorContaining(t, resp.Diagnostics, "requires Homarr 2.3.0 or newer")
}

// --- the header ------------------------------------------------------------------

// State written by an earlier version of the provider, or while Homarr
// predated 2.3.0, holds a null header. Planning that null would make the
// apply's read-back an inconsistent result.
func TestUserPlanLeavesANullPriorHeaderUnknown(t *testing.T) {
	prior := readUserState()
	prior.Header = types.ObjectNull(headerAttrTypes)
	config := userConfigOf(prior)
	config.ColorScheme = types.StringValue("light")

	planned, _ := planUser(t, prior, config)
	if !planned.Header.IsUnknown() {
		t.Fatalf("planned header = %v, want unknown", planned.Header)
	}

	// With nothing else changing there is no apply, and nothing to plan.
	planned, _ = planUser(t, prior, userConfigOf(prior))
	if !planned.Header.IsNull() {
		t.Fatalf("planned header = %v, want null kept in an empty plan", planned.Header)
	}
}

// The user exists and is configured; a header that cannot be read must not
// taint it, nor leave an unknown value in state.
func TestUserCreateTurnsAFailedHeaderReadIntoAWarning(t *testing.T) {
	fake := createdUserFake(t)
	fake.reply("GET /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	resp := runUserCreate(t, r, plan, plan)
	requireNoErrors(t, resp.Diagnostics)
	requireWarning(t, resp.Diagnostics, "boom")
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatal("state still holds unknown values")
	}
}

func TestUserHeaderReadFailureLeavesNoUnknownHeader(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)

	v := userPreferenceValues{Header: types.ObjectUnknown(headerAttrTypes)}
	var diags diag.Diagnostics
	readHeader(context.Background(), fake.client(t), "u1", &v, &diags)

	requireErrorContaining(t, diags, "boom")
	if !v.Header.IsNull() {
		t.Fatalf("header = %v, want null", v.Header)
	}
}
