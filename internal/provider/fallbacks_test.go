package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// These tests cover the paths the acceptance suite cannot reach against a
// current Homarr: the pre-2.3.0 fallbacks, ambiguous 404s and races. Homarr is
// faked in process through the client's Transport, so no port is opened.

// fakeHomarr answers requests by "METHOD /path"; unrouted requests get a 404.
type fakeHomarr struct {
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	calls    []string
	bodies   map[string]map[string]any
}

func newFakeHomarr() *fakeHomarr {
	return &fakeHomarr{handlers: map[string]http.HandlerFunc{}, bodies: map[string]map[string]any{}}
}

func (f *fakeHomarr) handle(route string, h http.HandlerFunc) { f.handlers[route] = h }

func (f *fakeHomarr) reply(route string, status int, body string) {
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *fakeHomarr) RoundTrip(req *http.Request) (*http.Response, error) {
	route := req.Method + " " + req.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, route)
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		var decoded map[string]any
		if json.Unmarshal(raw, &decoded) == nil {
			f.bodies[route] = decoded
		}
		req.Body = io.NopCloser(strings.NewReader(string(raw)))
	}
	h := f.handlers[route]
	f.mu.Unlock()

	rec := httptest.NewRecorder()
	if h == nil {
		rec.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rec, `{"message":"Not found","code":"NOT_FOUND"}`)
	} else {
		h(rec, req)
	}
	return rec.Result(), nil
}

func (f *fakeHomarr) called(route string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == route {
			return true
		}
	}
	return false
}

func (f *fakeHomarr) body(t *testing.T, route string) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bodies[route]
	if !ok {
		t.Fatalf("no request body recorded for %s; calls: %v", route, f.calls)
	}
	return b
}

func (f *fakeHomarr) client(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(client.Options{BaseURL: "http://homarr.test", APIKey: "id.token", Transport: f})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func resourceSchema(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	return resp.Schema
}

func stateOf(t *testing.T, s schema.Schema, model any) tfsdk.State {
	t.Helper()
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	if diags := state.Set(context.Background(), model); diags.HasError() {
		t.Fatalf("building state: %v", diags)
	}
	return state
}

func requireNoErrors(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("unexpected errors: %v", diags)
	}
}

func requireErrorContaining(t *testing.T, diags diag.Diagnostics, want string) {
	t.Helper()
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), want) || strings.Contains(d.Summary(), want) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got %v", want, diags)
}

// --- homarr_user ------------------------------------------------------------

func TestUserPreferencesFallbackKeepsTheUntouchedSlot(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"u","homeBoardId":"A","mobileHomeBoardId":"B"}`)
	fake.reply("PATCH /api/users/changeHome", 200, ``)
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", client.UserPreferencesPatch{"homeBoardId": "C"}, &diags)
	requireNoErrors(t, diags)

	body := fake.body(t, "PATCH /api/users/changeHome")
	if body["homeBoardId"] != "C" || body["mobileHomeBoardId"] != "B" {
		t.Fatalf("changeHome body = %v, want homeBoardId C and the untouched mobileHomeBoardId B", body)
	}
}

func TestUserPreferencesFallbackClearsOnlyTheNamedSlot(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"u","homeBoardId":"A","mobileHomeBoardId":"B"}`)
	fake.reply("PATCH /api/users/changeHome", 200, ``)
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", client.UserPreferencesPatch{"mobileHomeBoardId": nil}, &diags)
	requireNoErrors(t, diags)

	body := fake.body(t, "PATCH /api/users/changeHome")
	if body["homeBoardId"] != "A" || body["mobileHomeBoardId"] != nil {
		t.Fatalf("changeHome body = %v, want homeBoardId A kept and mobileHomeBoardId null", body)
	}
}

func TestUserPreferences404ForAMissingUserIsNotBlamedOnTheVersion(t *testing.T) {
	fake := newFakeHomarr() // both the PATCH and the user read 404
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", client.UserPreferencesPatch{"colorScheme": "dark"}, &diags)

	requireErrorContaining(t, diags, "deleted outside Terraform")
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "predates 2.3.0") {
			t.Fatalf("a missing user was reported as an old Homarr: %v", diags)
		}
	}
	if fake.called("PATCH /api/users/changeHome") {
		t.Fatal("fell back to the legacy endpoint for a user that does not exist")
	}
}

func TestUserPreferencesOnOldHomarrRejectsNonHomeFields(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"u"}`)
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", client.UserPreferencesPatch{"colorScheme": "dark"}, &diags)
	requireErrorContaining(t, diags, "predates 2.3.0")
}

func TestPreferencesPatchClearsNullableReferences(t *testing.T) {
	prior := userPreferenceValues{
		HomeBoardID:           types.StringValue("A"),
		MobileHomeBoardID:     types.StringValue("B"),
		DefaultSearchEngineID: types.StringValue("S"),
	}
	plan := userPreferenceValues{
		HomeBoardID:           types.StringNull(),    // homarr_user: omitted means none
		MobileHomeBoardID:     types.StringValue(""), // homarr_user_preferences: "" means none
		DefaultSearchEngineID: types.StringUnknown(), // not configured, left alone
	}

	patch := preferencesPatch(plan, prior)
	for _, key := range []string{"homeBoardId", "mobileHomeBoardId"} {
		v, ok := patch[key]
		if !ok || v != nil {
			t.Fatalf("patch[%q] = %v (present %v), want an explicit null", key, v, ok)
		}
	}
	if _, ok := patch["defaultSearchEngineId"]; ok {
		t.Fatalf("an unknown reference was patched: %v", patch)
	}
}

func TestUserNullableReferencesAreNotComputed(t *testing.T) {
	s := resourceSchema(t, &userResource{})
	for _, name := range []string{"home_board_id", "mobile_home_board_id", "default_search_engine_id"} {
		attr := s.Attributes[name]
		if attr.IsComputed() {
			t.Errorf("%s is computed, so a null in configuration could never clear it", name)
		}
	}
}

// --- homarr_user_preferences ------------------------------------------------

func TestUserPreferencesKeepsEmptyStringForAClearedReference(t *testing.T) {
	m := userPreferencesModel{userPreferenceValues: userPreferenceValues{
		HomeBoardID:       types.StringValue(""),
		MobileHomeBoardID: types.StringUnknown(),
	}}
	m.fromAPI(&client.UserPreferences{UserID: "u1"})

	if m.HomeBoardID.IsNull() || m.HomeBoardID.ValueString() != "" {
		t.Fatalf("home_board_id = %v, want the configured \"\" kept", m.HomeBoardID)
	}
	if !m.MobileHomeBoardID.IsNull() {
		t.Fatalf("mobile_home_board_id = %v, want null for an unconfigured, unset reference", m.MobileHomeBoardID)
	}
}

func userPreferencesReadState(t *testing.T) (tfsdk.State, schema.Schema) {
	t.Helper()
	s := resourceSchema(t, &userPreferencesResource{})
	return stateOf(t, s, &userPreferencesModel{
		ID:     types.StringValue("u1"),
		UserID: types.StringValue("u1"),
		userPreferenceValues: userPreferenceValues{
			ColorScheme:               types.StringValue("dark"),
			ByteUnitSystem:            types.StringValue("binary"),
			FirstDayOfWeek:            types.Int64Value(1),
			PingIconsEnabled:          types.BoolValue(false),
			EnableRightClickOnWidgets: types.BoolValue(false),
			HomeBoardID:               types.StringNull(),
			MobileHomeBoardID:         types.StringNull(),
			DefaultSearchEngineID:     types.StringNull(),
			OpenSearchInNewTab:        types.BoolValue(true),
			DdgBangs:                  types.BoolValue(true),
		},
	}), s
}

func TestUserPreferencesReadKeepsStateWhenOnlyTheEndpointIsMissing(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"u"}`)
	r := &userPreferencesResource{client: fake.client(t)}

	state, _ := userPreferencesReadState(t)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	requireNoErrors(t, resp.Diagnostics)
	if resp.State.Raw.IsNull() {
		t.Fatal("state was dropped although the user still exists")
	}
	if resp.Diagnostics.WarningsCount() == 0 {
		t.Fatal("expected a warning that the preferences could not be refreshed")
	}
}

func TestUserPreferencesReadDropsStateWhenTheUserIsGone(t *testing.T) {
	fake := newFakeHomarr() // preferences and user both 404
	r := &userPreferencesResource{client: fake.client(t)}

	state, _ := userPreferencesReadState(t)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	requireNoErrors(t, resp.Diagnostics)
	if !resp.State.Raw.IsNull() {
		t.Fatal("state was kept for a user that no longer exists")
	}
}

// --- homarr_board_settings --------------------------------------------------

func TestBoardSettingsFallbackLeavesNoUnknowns(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/settings", 200, `{}`)
	// GET /api/boards/b1/settings is unrouted: a Homarr older than 2.3.0.
	r := &boardSettingsResource{client: fake.client(t)}

	plan := boardSettingsModel{
		ID:      types.StringUnknown(),
		BoardID: types.StringValue("b1"),
		boardSettingsValues: boardSettingsValues{
			PageTitle:                 types.StringValue("Infra"),
			MetaTitle:                 types.StringUnknown(),
			LogoImageURL:              types.StringUnknown(),
			FaviconImageURL:           types.StringUnknown(),
			BackgroundImageURL:        types.StringUnknown(),
			BackgroundImageAttachment: types.StringUnknown(),
			BackgroundImageRepeat:     types.StringUnknown(),
			BackgroundImageSize:       types.StringUnknown(),
			PrimaryColor:              types.StringUnknown(),
			SecondaryColor:            types.StringUnknown(),
			Opacity:                   types.Int64Unknown(),
			IconColor:                 types.StringUnknown(),
			ItemRadius:                types.StringUnknown(),
			CustomCSS:                 types.StringUnknown(),
			DisableStatus:             types.BoolUnknown(),
		},
	}

	var diags diag.Diagnostics
	if err := r.apply(context.Background(), &plan, &diags); err != nil {
		t.Fatal(err)
	}

	state := stateOf(t, resourceSchema(t, r), &plan)
	if !state.Raw.IsFullyKnown() {
		t.Fatalf("state still holds unknown values after the write-only fallback: %v", state.Raw)
	}
	if plan.PageTitle.ValueString() != "Infra" {
		t.Fatalf("page_title = %v, want the configured value kept", plan.PageTitle)
	}
}

func TestBoardSettingsKeepsEmptyStringsThatReadBackAsNull(t *testing.T) {
	m := boardSettingsValues{
		IconColor: types.StringValue(""),
		PageTitle: types.StringValue(""),
		MetaTitle: types.StringUnknown(),
	}
	m.fromAPI(&client.BoardSettingsRead{ID: "b1"})

	if m.IconColor.IsNull() || m.PageTitle.IsNull() {
		t.Fatalf("icon_color = %v, page_title = %v; want the configured \"\" kept", m.IconColor, m.PageTitle)
	}
	if !m.MetaTitle.IsNull() {
		t.Fatalf("meta_title = %v, want null for an unconfigured, unset field", m.MetaTitle)
	}
}

func TestBoardSettingsUpgradeTruncatesFractionalOpacity(t *testing.T) {
	ctx := context.Background()
	r := &boardSettingsResource{}
	upgrader := r.UpgradeState(ctx)[0]

	prior := tfsdk.State{
		Schema: *upgrader.PriorSchema,
		Raw:    tftypes.NewValue(upgrader.PriorSchema.Type().TerraformType(ctx), nil),
	}
	for name, value := range map[string]any{
		"id":         types.StringValue("b1"),
		"board_id":   types.StringValue("b1"),
		"page_title": types.StringValue("Infra"),
		"opacity":    types.Float64Value(50.5),
	} {
		if diags := prior.SetAttribute(ctx, path.Root(name), value); diags.HasError() {
			t.Fatalf("building prior state: %v", diags)
		}
	}

	resp := resource.UpgradeStateResponse{State: tfsdk.State{Schema: resourceSchema(t, r)}}
	upgrader.StateUpgrader(ctx, resource.UpgradeStateRequest{State: &prior}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	var upgraded boardSettingsModel
	requireNoErrors(t, resp.State.Get(ctx, &upgraded))
	if upgraded.Opacity.ValueInt64() != 50 {
		t.Fatalf("opacity = %v, want 50", upgraded.Opacity)
	}
	if upgraded.PageTitle.ValueString() != "Infra" || upgraded.BoardID.ValueString() != "b1" {
		t.Fatalf("other attributes were not carried over: %+v", upgraded)
	}
}

// --- homarr_board -----------------------------------------------------------

func TestBoardCreateClearsAnAutomaticHomeBoardWhenAskedFor(t *testing.T) {
	fake := newFakeHomarr()
	// Homarr made the freshly created board home on its own.
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"b","isHome":true,"isMobileHome":false}]`)
	fake.reply("PATCH /api/users/preferences", 200, `{"userId":"me"}`)
	r := &boardResource{client: fake.client(t)}

	var diags diag.Diagnostics
	plan := boardModel{IsHome: types.BoolValue(false), IsMobileHome: types.BoolValue(false)}
	r.applyHomeFlags(context.Background(), "b1", plan, &diags)
	requireNoErrors(t, diags)

	body := fake.body(t, "PATCH /api/users/preferences")
	if v, ok := body["homeBoardId"]; !ok || v != nil {
		t.Fatalf("preferences body = %v, want an explicit null homeBoardId", body)
	}
	if _, ok := body["mobileHomeBoardId"]; ok {
		t.Fatalf("cleared the mobile slot although this board does not hold it: %v", body)
	}
}

func TestBoardDoesNotClearAHomeBoardItDoesNotHold(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"b","isHome":false,"isMobileHome":false}]`)
	r := &boardResource{client: fake.client(t)}

	var diags diag.Diagnostics
	// Prior state may still claim the flag; only the server's answer counts.
	plan := boardModel{IsHome: types.BoolValue(false), IsMobileHome: types.BoolUnknown()}
	r.applyHomeFlags(context.Background(), "b1", plan, &diags)
	requireNoErrors(t, diags)

	if fake.called("PATCH /api/users/preferences") {
		t.Fatal("cleared the user's home board although this board no longer held it")
	}
}

func TestBoardClearErrorOnlyBlamesTheVersionFor404(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"b","isHome":true}]`)
	fake.reply("PATCH /api/users/preferences", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &boardResource{client: fake.client(t)}

	var diags diag.Diagnostics
	plan := boardModel{IsHome: types.BoolValue(false), IsMobileHome: types.BoolUnknown()}
	r.applyHomeFlags(context.Background(), "b1", plan, &diags)

	requireErrorContaining(t, diags, "boom")
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), "2.3.0") {
			t.Fatalf("a server error was blamed on the Homarr version: %v", diags)
		}
	}
}

// TestBoardMovingTheHomeFlagDoesNotLoseIt runs the "move the flag from B to A"
// apply: A selects while B clears, concurrently as Terraform would. The board
// read is slowed down so that, without serialisation, A's select lands between
// B's check and B's clear.
func TestBoardMovingTheHomeFlagDoesNotLoseIt(t *testing.T) {
	var mu sync.Mutex
	home := "B"

	fake := newFakeHomarr()
	fake.handle("GET /api/boards", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		current := home
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "A", "name": "a", "isHome": current == "A"},
			{"id": "B", "name": "b", "isHome": current == "B"},
		})
	})
	fake.handle("PATCH /api/boards/A/home", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		home = "A"
		mu.Unlock()
	})
	fake.handle("PATCH /api/users/preferences", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		home = ""
		mu.Unlock()
		_, _ = io.WriteString(w, `{"userId":"me"}`)
	})
	r := &boardResource{client: fake.client(t)}

	var wg sync.WaitGroup
	var diagsA, diagsB diag.Diagnostics
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.applyHomeFlags(context.Background(), "B",
			boardModel{IsHome: types.BoolValue(false), IsMobileHome: types.BoolUnknown()}, &diagsB)
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // B is already reading the board
		r.applyHomeFlags(context.Background(), "A",
			boardModel{IsHome: types.BoolValue(true), IsMobileHome: types.BoolUnknown()}, &diagsA)
	}()
	wg.Wait()

	requireNoErrors(t, diagsA)
	requireNoErrors(t, diagsB)
	if home != "A" {
		t.Fatalf("home board = %q after moving it to A", home)
	}
}

func TestBoardSelectsTheHomeBoardEvenWhenStateAlreadyClaimsIt(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/home", 200, ``)
	r := &boardResource{client: fake.client(t)}

	// Whatever prior state says, a plan asking for the flag gets the idempotent
	// select: state can be stale after -refresh=false or an out-of-band change.
	var diags diag.Diagnostics
	r.applyHomeFlags(context.Background(), "b1",
		boardModel{IsHome: types.BoolValue(true), IsMobileHome: types.BoolUnknown()}, &diags)
	requireNoErrors(t, diags)

	if !fake.called("PATCH /api/boards/b1/home") {
		t.Fatal("the select was skipped")
	}
}

func planOf(t *testing.T, s schema.Schema, model any) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	if diags := plan.Set(context.Background(), model); diags.HasError() {
		t.Fatalf("building plan: %v", diags)
	}
	return plan
}

// Creating a board can make it the home board as a side effect, so the create
// itself must run under the same lock as every select and clear.
func TestBoardCreateHoldsTheHomeBoardLock(t *testing.T) {
	lockHeld := false
	fake := newFakeHomarr()
	fake.handle("POST /api/boards", func(w http.ResponseWriter, _ *http.Request) {
		if homeBoardMu.TryLock() {
			homeBoardMu.Unlock()
		} else {
			lockHeld = true
		}
		_, _ = io.WriteString(w, `{"boardId":"b1"}`)
	})
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"tfacc","isHome":true}]`)
	r := &boardResource{client: fake.client(t)}

	s := resourceSchema(t, r)
	plan := planOf(t, s, &boardModel{
		ID:           types.StringUnknown(),
		Name:         types.StringValue("tfacc"),
		ColumnCount:  types.Int64Value(10),
		IsPublic:     types.BoolValue(false),
		LogoImageURL: types.StringUnknown(),
		CreatorID:    types.StringUnknown(),
		IsHome:       types.BoolUnknown(),
		IsMobileHome: types.BoolUnknown(),
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: plan}, &resp)

	requireNoErrors(t, resp.Diagnostics)
	if !lockHeld {
		t.Fatal("POST /api/boards ran without holding homeBoardMu")
	}
}

// --- empty settings and whitespace -------------------------------------------

func boardIDOnlyPlan() boardSettingsModel {
	m := boardSettingsModel{ID: types.StringUnknown(), BoardID: types.StringValue("b1")}
	m.PageTitle, m.MetaTitle, m.LogoImageURL = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	m.FaviconImageURL, m.BackgroundImageURL = types.StringUnknown(), types.StringUnknown()
	m.BackgroundImageAttachment, m.BackgroundImageRepeat = types.StringUnknown(), types.StringUnknown()
	m.BackgroundImageSize, m.PrimaryColor, m.SecondaryColor = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	m.IconColor, m.ItemRadius, m.CustomCSS = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	m.Opacity, m.DisableStatus = types.Int64Unknown(), types.BoolUnknown()
	return m
}

func TestBoardSettingsWithOnlyABoardIDSendsNoPatch(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards/b1/settings", 200, `{"id":"b1","name":"b","backgroundImageAttachment":"fixed",`+
		`"backgroundImageRepeat":"no-repeat","backgroundImageSize":"cover","primaryColor":"#fa5252",`+
		`"secondaryColor":"#fd7e14","opacity":100,"itemRadius":"lg","customCss":"","disableStatus":false}`)
	r := &boardSettingsResource{client: fake.client(t)}

	plan := boardIDOnlyPlan()
	var diags diag.Diagnostics
	if err := r.apply(context.Background(), &plan, &diags); err != nil {
		t.Fatal(err)
	}
	if fake.called("PATCH /api/boards/b1/settings") {
		t.Fatal("sent an empty settings patch")
	}
	if plan.BackgroundImageSize.ValueString() != "cover" || plan.Opacity.ValueInt64() != 100 {
		t.Fatalf("settings were not read back: %+v", plan.boardSettingsValues)
	}
}

func TestBoardSettingsWithOnlyABoardIDStillReportsAMissingBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[]`) // settings GET is unrouted, so 404s too
	r := &boardSettingsResource{client: fake.client(t)}

	plan := boardIDOnlyPlan()
	var diags diag.Diagnostics
	err := r.apply(context.Background(), &plan, &diags)
	if !client.IsNotFound(err) {
		t.Fatalf("apply() error = %v, want a not-found error for the missing board", err)
	}
}

// --- refused home boards and non-admin keys --------------------------------

const boardNotFoundBody = `{"message":"Board not found","code":"NOT_FOUND"}`

func TestUserPreferencesReportARefusedHomeBoardAgainstTheAttribute(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 404, boardNotFoundBody)
	r := &userResource{client: fake.client(t)}

	var diags diag.Diagnostics
	r.applyPreferences(context.Background(), "u1", client.UserPreferencesPatch{"homeBoardId": "nope"}, &diags)

	if diags.ErrorsCount() != 1 {
		t.Fatalf("want exactly one error, got %v", diags)
	}
	withPath, ok := diags.Errors()[0].(diag.DiagnosticWithPath)
	if !ok || !withPath.Path().Equal(path.Root("home_board_id")) {
		t.Fatalf("error is not attached to home_board_id: %v", diags)
	}
	if fake.called("GET /api/users/u1") || fake.called("PATCH /api/users/changeHome") {
		t.Fatal("a refused board was treated as a missing user or an old Homarr")
	}
}

func TestUserPreferencesResourceReportsARefusedHomeBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 404, boardNotFoundBody)
	r := &userPreferencesResource{client: fake.client(t)}

	s := resourceSchema(t, r)
	model := &userPreferencesModel{ID: types.StringUnknown(), UserID: types.StringValue("u1")}
	model.HomeBoardID, model.MobileHomeBoardID = types.StringValue("nope"), types.StringValue("also-nope")
	model.DefaultSearchEngineID, model.ColorScheme, model.ByteUnitSystem = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	model.FirstDayOfWeek = types.Int64Unknown()
	model.PingIconsEnabled, model.EnableRightClickOnWidgets = types.BoolUnknown(), types.BoolUnknown()
	model.OpenSearchInNewTab, model.DdgBangs = types.BoolUnknown(), types.BoolUnknown()

	resp := resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}}
	r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, s, model)}, &resp)

	// Both slots were set, and Homarr does not say which it refused.
	requireErrorContaining(t, resp.Diagnostics, "Home board not found")
	for _, d := range resp.Diagnostics.Errors() {
		if strings.Contains(d.Summary(), "User not found") {
			t.Fatalf("a refused board was reported as a missing user: %v", resp.Diagnostics)
		}
	}
}

func TestUserPreferencesReadExplainsANonAdminKey(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 403, `{"message":"Forbidden","code":"FORBIDDEN"}`)
	r := &userPreferencesResource{client: fake.client(t)}

	state, _ := userPreferencesReadState(t)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)

	requireErrorContaining(t, resp.Diagnostics, "not an admin")
	if resp.State.Raw.IsNull() {
		t.Fatal("state was dropped for a user the key merely cannot see")
	}
}
