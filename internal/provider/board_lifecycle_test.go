package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests cover homarr_board's behaviour around access errors, name
// clashes and the base layout id, against Homarr faked in process.

const listedBoardB1 = `[{"id":"b1","name":"infra","isPublic":true,"creator":{"id":"u1","name":"admin","image":null},` +
	`"isHome":false,"isMobileHome":false}]`

func boardStateModel(name string, isPublic bool) *boardModel {
	return &boardModel{
		ID:           types.StringValue("b1"),
		Name:         types.StringValue(name),
		ColumnCount:  types.Int64Value(10),
		IsPublic:     types.BoolValue(isPublic),
		LogoImageURL: types.StringNull(),
		CreatorID:    types.StringValue("u1"),
		IsHome:       types.BoolValue(false),
		IsMobileHome: types.BoolValue(false),
		BaseLayoutID: types.StringValue("l1"),
	}
}

func nullStateFor(t *testing.T, r resource.Resource) tfsdk.State {
	t.Helper()
	s := resourceSchema(t, r)
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func requireNoErrorContaining(t *testing.T, diags diag.Diagnostics, unwanted string) {
	t.Helper()
	for _, d := range diags.Errors() {
		if strings.Contains(d.Detail(), unwanted) || strings.Contains(d.Summary(), unwanted) {
			t.Fatalf("error mentions %q although it should not: %v", unwanted, diags)
		}
	}
}

func requireAttributeError(t *testing.T, diags diag.Diagnostics, attribute string) {
	t.Helper()
	for _, d := range diags.Errors() {
		if withPath, ok := d.(diag.DiagnosticWithPath); ok && withPath.Path().Equal(path.Root(attribute)) {
			return
		}
	}
	t.Fatalf("expected an error attached to %s, got %v", attribute, diags)
}

func requireAttributeWarning(t *testing.T, diags diag.Diagnostics, attribute string) {
	t.Helper()
	for _, d := range diags.Warnings() {
		if withPath, ok := d.(diag.DiagnosticWithPath); ok && withPath.Path().Equal(path.Root(attribute)) {
			return
		}
	}
	t.Fatalf("expected a warning attached to %s, got %v", attribute, diags)
}

// boardRouteCalls counts the requests the fake received for route.
func boardRouteCalls(f *fakeHomarr, route string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == route {
			n++
		}
	}
	return n
}

func deleteBoardState(t *testing.T, r *boardResource) resource.DeleteRequest {
	t.Helper()
	return resource.DeleteRequest{State: stateOf(t, resourceSchema(t, r), boardStateModel("infra", true))}
}

// --- delete -------------------------------------------------------------------

func TestBoardDeleteWithoutFullAccessKeepsTheBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/b1", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, listedBoardB1)
	r := &boardResource{client: fake.client(t)}

	resp := resource.DeleteResponse{State: deleteBoardState(t, r).State}
	r.Delete(context.Background(), deleteBoardState(t, r), &resp)

	requireErrorContaining(t, resp.Diagnostics, "deleting a board requires full access")
}

func TestBoardDeleteTreatsAnUnlistedBoardAsGone(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/b1", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, `[]`)
	r := &boardResource{client: fake.client(t)}

	resp := resource.DeleteResponse{State: deleteBoardState(t, r).State}
	r.Delete(context.Background(), deleteBoardState(t, r), &resp)

	requireNoErrors(t, resp.Diagnostics)
}

func TestBoardDeleteWarnsWhenTheBoardWasTheInstanceHomeBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/b1", 200, ``)
	fake.reply("GET /api/settings/board", 200,
		`{"homeBoardId":"b1","mobileHomeBoardId":"b1","enableStatusByDefault":true,"forceDisableStatus":false}`)
	r := &boardResource{client: fake.client(t)}

	resp := resource.DeleteResponse{State: deleteBoardState(t, r).State}
	r.Delete(context.Background(), deleteBoardState(t, r), &resp)

	requireNoErrors(t, resp.Diagnostics)
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("want one warning about the dangling home board, got %v", resp.Diagnostics)
	}
	detail := resp.Diagnostics.Warnings()[0].Detail()
	if !strings.Contains(detail, "home_board_id and mobile_home_board_id") {
		t.Fatalf("warning does not name both slots: %s", detail)
	}
}

func TestBoardDeleteSkipsTheHomeBoardCheckForNonAdminKeys(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/b1", 200, ``)
	fake.reply("GET /api/settings/board", 403, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	r := &boardResource{client: fake.client(t)}

	resp := resource.DeleteResponse{State: deleteBoardState(t, r).State}
	r.Delete(context.Background(), deleteBoardState(t, r), &resp)

	if len(resp.Diagnostics) != 0 {
		t.Fatalf("want no diagnostics for a key that cannot read the server settings, got %v", resp.Diagnostics)
	}
}

func TestBoardDeleteDoesNotWarnAboutAnotherHomeBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/b1", 200, ``)
	fake.reply("GET /api/settings/board", 200,
		`{"homeBoardId":"b2","mobileHomeBoardId":null,"enableStatusByDefault":true,"forceDisableStatus":false}`)
	r := &boardResource{client: fake.client(t)}

	resp := resource.DeleteResponse{State: deleteBoardState(t, r).State}
	r.Delete(context.Background(), deleteBoardState(t, r), &resp)

	if len(resp.Diagnostics) != 0 {
		t.Fatalf("want no diagnostics, got %v", resp.Diagnostics)
	}
}

// --- update -------------------------------------------------------------------

func updateBoard(t *testing.T, r *boardResource, prior, planned *boardModel) resource.UpdateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	resp := resource.UpdateResponse{State: nullStateFor(t, r)}
	r.Update(context.Background(), resource.UpdateRequest{Plan: planOf(t, s, planned), State: stateOf(t, s, prior)}, &resp)
	return resp
}

func TestBoardRenameWithoutFullAccessSaysSo(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/name", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, listedBoardB1)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra2", true))

	requireErrorContaining(t, resp.Diagnostics, "renaming a board requires full access")
}

func TestBoardRenameOfADeletedBoardSaysItIsGone(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/name", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, `[]`)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra2", true))

	requireErrorContaining(t, resp.Diagnostics, "deleted outside Terraform")
	requireNoErrorContaining(t, resp.Diagnostics, "full access")
}

func TestBoardVisibility404IsNotBlamedOnTheHomeBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/visibility", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, listedBoardB1)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra", false))

	requireErrorContaining(t, resp.Diagnostics, "changing the visibility of a board requires full access")
	requireNoErrorContaining(t, resp.Diagnostics, "instance-wide desktop or mobile home board")
}

func TestBoardVisibilityRefusalForTheInstanceHomeBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/visibility", 400, `{"message":"Cannot make home board private","code":"BAD_REQUEST"}`)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra", false))

	requireErrorContaining(t, resp.Diagnostics, "instance-wide desktop or mobile home board")
	requireAttributeError(t, resp.Diagnostics, "is_public")
}

func TestBoardVisibilityServerErrorIsReportedAsIs(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/visibility", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra", false))

	requireErrorContaining(t, resp.Diagnostics, "boom")
	requireNoErrorContaining(t, resp.Diagnostics, "instance-wide desktop or mobile home board")
}

func TestBoardUpdateKeepsTheBaseLayoutID(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/boards/b1/name", 200, ``)
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"infra2","isPublic":true,"isHome":false,"isMobileHome":false}]`)
	r := &boardResource{client: fake.client(t)}

	resp := updateBoard(t, r, boardStateModel("infra", true), boardStateModel("infra2", true))
	requireNoErrors(t, resp.Diagnostics)

	var got boardModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.BaseLayoutID.ValueString() != "l1" || got.Name.ValueString() != "infra2" {
		t.Fatalf("state after update = %+v, want the base layout id kept and the new name", got)
	}
}

// --- create -------------------------------------------------------------------

func createBoardPlan(name string) *boardModel {
	return &boardModel{
		ID:           types.StringUnknown(),
		Name:         types.StringValue(name),
		ColumnCount:  types.Int64Value(10),
		IsPublic:     types.BoolValue(false),
		LogoImageURL: types.StringUnknown(),
		CreatorID:    types.StringUnknown(),
		IsHome:       types.BoolUnknown(),
		IsMobileHome: types.BoolUnknown(),
		BaseLayoutID: types.StringUnknown(),
	}
}

func createBoard(t *testing.T, r *boardResource, plan *boardModel) resource.CreateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: nullStateFor(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, s, plan)}, &resp)
	return resp
}

func TestBoardCreateRecordsTheBaseLayoutID(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards", 200, `{"boardId":"b1","name":"infra","layoutId":"l1"}`)
	fake.reply("GET /api/boards", 200, listedBoardB1)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))
	requireNoErrors(t, resp.Diagnostics)

	var got boardModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.BaseLayoutID.ValueString() != "l1" {
		t.Fatalf("base_layout_id = %v, want l1 from the create response", got.BaseLayoutID)
	}
}

// Homarr's REST create leaves name uniqueness to the database, whose violation
// arrives as an internal error rather than the 409 rename gives. The exact
// duplicate passes the provider's own check before the POST, which leaves it
// to Homarr.
func TestBoardCreateReportsAUniqueConstraintFailureAsATakenName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, listedBoardB1)
	fake.reply("POST /api/boards", 500, `{"message":"UNIQUE constraint failed: board.name","code":"INTERNAL_SERVER_ERROR"}`)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))

	requireAttributeError(t, resp.Diagnostics, "name")
	requireErrorContaining(t, resp.Diagnostics, "Board name already taken")
	if n := boardRouteCalls(fake, "GET /api/boards"); n != 1 {
		t.Fatalf("listed boards %d times, want only the check before the POST: the error already identified the clash", n)
	}
}

func TestBoardCreateRelistsAfterAnUnexplainedServerError(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards", 500, `{"message":"Internal server error","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("GET /api/boards", 200, `[{"id":"other","name":"infra","isPublic":false,"isHome":false,"isMobileHome":false}]`)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))

	requireAttributeError(t, resp.Diagnostics, "name")
	requireErrorContaining(t, resp.Diagnostics, `board "infra" (id other)`)
}

// POST /api/boards would accept a name differing only in case; the provider
// refuses it before anything is created.
func TestBoardCreateRefusesACaseVariantBeforeCreating(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("Infra"))

	requireAttributeError(t, resp.Diagnostics, "name")
	requireErrorContaining(t, resp.Diagnostics, `"infra" (id b1)`)
	if fake.called("POST /api/boards") {
		t.Fatal("created the board although its name differs from another board's only in case")
	}
}

// statefulBoards is a fake board list that POST /api/boards adds to, with the
// case-sensitive unique constraint Homarr's databases enforce. The listing is
// taken, then answered only after a pause, so that a create racing another one
// would see the list as it was before either board existed.
func statefulBoards(fake *fakeHomarr) {
	var mu sync.Mutex
	var boards []map[string]any
	fake.handle("GET /api/boards", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		snapshot := []byte(`[]`)
		if len(boards) > 0 {
			snapshot, _ = json.Marshal(boards)
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(snapshot)
	})
	fake.handle("POST /api/boards", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		for _, b := range boards {
			if b["name"] == body.Name {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"UNIQUE constraint failed: board.name","code":"INTERNAL_SERVER_ERROR"}`)
				return
			}
		}
		id := fmt.Sprintf("b%d", len(boards)+1)
		boards = append(boards, map[string]any{"id": id, "name": body.Name, "isPublic": false,
			"isHome": false, "isMobileHome": false})
		_, _ = fmt.Fprintf(w, `{"boardId":%q,"layoutId":"l-%s"}`, id, id)
	})
}

// Terraform creates independent boards in parallel. Two whose names differ only
// in case must not both pass the check before either exists: Homarr would
// accept both.
func TestBoardCreateRefusesACaseVariantCreatedConcurrently(t *testing.T) {
	fake := newFakeHomarr()
	statefulBoards(fake)
	r := &boardResource{client: fake.client(t)}

	names := []string{"Ops", "ops"}
	responses := make([]resource.CreateResponse, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses[i] = createBoard(t, r, createBoardPlan(name))
		}()
	}
	wg.Wait()

	created, refused := 0, 0
	for _, resp := range responses {
		if !resp.Diagnostics.HasError() {
			created++
			continue
		}
		requireAttributeError(t, resp.Diagnostics, "name")
		requireErrorContaining(t, resp.Diagnostics, "Board name already taken")
		refused++
	}
	if created != 1 || refused != 1 {
		t.Fatalf("%d boards created and %d refused, want exactly one of each: %v and %v",
			created, refused, responses[0].Diagnostics, responses[1].Diagnostics)
	}
	if n := boardRouteCalls(fake, "POST /api/boards"); n != 1 {
		t.Fatalf("POST /api/boards sent %d times, want only the first board's", n)
	}
}

func TestBoardCreateSkipsTheNameCheckWhenListingFails(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("POST /api/boards", 200, `{"boardId":"b1","name":"infra","layoutId":"l1"}`)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))

	if !fake.called("POST /api/boards") {
		t.Fatal("a failed listing stopped the create")
	}
	// The read-back fails on the same listing and taints the board; the name
	// check itself added nothing.
	requireNoErrorContaining(t, resp.Diagnostics, "already taken")
}

func TestBoardCreateKeepsUnrelatedServerErrors(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards", 500, `{"message":"database is locked","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("GET /api/boards", 200, `[]`)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))

	requireErrorContaining(t, resp.Diagnostics, "Unable to create Homarr board")
	requireNoErrorContaining(t, resp.Diagnostics, "already taken")
}

func TestBoardCreateKeepsTheBoardInStateWhenTheReadBackFails(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards", 200, `{"boardId":"b1","name":"infra","layoutId":"l1"}`)
	fake.reply("GET /api/boards", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	r := &boardResource{client: fake.client(t)}

	resp := createBoard(t, r, createBoardPlan("infra"))

	requireErrorContaining(t, resp.Diagnostics, "boom")
	var got boardModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "b1" {
		t.Fatalf("state id = %v, want the created board kept so that it is tainted, not leaked", got.ID)
	}
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatalf("state holds unknown values: %v", resp.State.Raw)
	}
}

// --- plan-time name check -----------------------------------------------------

// The plan-time check only ever warns: a plan cannot tell a real clash from a
// board renamed or destroyed earlier in the same apply, or from the tainted
// board it replaces.

// fakePrivate stands in for the framework's private state, whose type is
// internal to the framework.
type fakePrivate map[string][]byte

func (p fakePrivate) GetKey(_ context.Context, key string) ([]byte, diag.Diagnostics) {
	return p[key], nil
}

func (p fakePrivate) SetKey(_ context.Context, key string, value []byte) diag.Diagnostics {
	if len(value) == 0 {
		delete(p, key)
		return nil
	}
	p[key] = value
	return nil
}

const boardsInfraAndMedia = `[` +
	`{"id":"b1","name":"infra","isPublic":true,"isHome":false,"isMobileHome":false},` +
	`{"id":"b2","name":"media","isPublic":true,"isHome":false,"isMobileHome":false}]`

func TestBoardNameCheckWarnsAboutACaseVariantOnCreate(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("Infra"), types.StringNull(), types.StringNull(), fakePrivate{}, &diags)

	requireNoErrors(t, diags)
	requireAttributeWarning(t, diags, "name")
	if detail := diags.Warnings()[0].Detail(); !strings.Contains(detail, `"infra" (id b1)`) {
		t.Fatalf("warning does not name the board holding the name: %s", detail)
	}
}

// Without prior or private state, a tainted board being replaced is planned
// exactly like a fresh create, and its own board is listed under its name.
// That must not fail the plan, or the replacement could never happen.
func TestBoardNameCheckDoesNotRefuseATaintedReplacement(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("infra"), types.StringNull(), types.StringNull(), fakePrivate{}, &diags)

	requireNoErrors(t, diags)
	requireAttributeWarning(t, diags, "name")
}

func TestBoardNameCheckAllowsAFreeName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("music"), types.StringNull(), types.StringNull(), fakePrivate{}, &diags)

	if len(diags) != 0 {
		t.Fatalf("want no diagnostics for a free name, got %v", diags)
	}
}

// Renaming a board to a different spelling of its own name is fine: Homarr
// ignores the board itself, and so must the check.
func TestBoardNameCheckIgnoresTheBoardBeingRenamed(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("INFRA"), types.StringValue("b1"), types.StringValue("infra"), fakePrivate{}, &diags)

	if len(diags) != 0 {
		t.Fatalf("want no diagnostics for a board's own name, got %v", diags)
	}
}

// Homarr refuses the rename itself unless the other board moves away earlier
// in the same apply, so the plan only warns.
func TestBoardNameCheckWarnsAboutARenameOntoAnotherBoard(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("Media"), types.StringValue("b1"), types.StringValue("infra"), fakePrivate{}, &diags)

	requireNoErrors(t, diags)
	requireAttributeWarning(t, diags, "name")
}

func TestBoardNameCheckSkipsAnUnchangedName(t *testing.T) {
	fake := newFakeHomarr()

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("infra"), types.StringValue("b1"), types.StringValue("infra"), fakePrivate{}, &diags)

	requireNoErrors(t, diags)
	if fake.called("GET /api/boards") {
		t.Fatal("listed boards for a plan that does not change the name")
	}
}

// Replacing a board plans the new object a second time without prior state.
// The board being replaced, which usually has the same name, must not count as
// a clash then.
func TestBoardNameCheckIgnoresTheBoardBeingReplaced(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)
	c := fake.client(t)
	private := fakePrivate{}

	// First plan, against the existing board: no rename, so nothing to check,
	// but the board's id is handed over in private state.
	var first diag.Diagnostics
	checkPlannedBoardName(context.Background(), c,
		types.StringValue("infra"), types.StringValue("b1"), types.StringValue("infra"), private, &first)
	requireNoErrors(t, first)

	// Second plan, for the replacement, with the first plan's private state.
	var second diag.Diagnostics
	checkPlannedBoardName(context.Background(), c,
		types.StringValue("infra"), types.StringNull(), types.StringNull(), private, &second)
	if len(second) != 0 {
		t.Fatalf("the replacement was warned about its own board: %v", second)
	}

	if _, ok := private[replacedBoardKey]; ok {
		t.Fatal("the replaced board's id was carried into the new object's private state")
	}
}

func TestBoardNameCheckIsBestEffort(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)

	var diags diag.Diagnostics
	checkPlannedBoardName(context.Background(), fake.client(t),
		types.StringValue("infra"), types.StringNull(), types.StringNull(), fakePrivate{}, &diags)

	if len(diags) != 0 {
		t.Fatalf("a failed listing should not affect the plan, got %v", diags)
	}
}

// ModifyPlan feeds the planned name and the prior state into the check.
func TestBoardModifyPlanChecksTheName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)
	r := &boardResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	plan := planOf(t, s, createBoardPlan("MEDIA"))
	req := resource.ModifyPlanRequest{Plan: plan, State: nullStateFor(t, r)}
	resp := resource.ModifyPlanResponse{Plan: plan}
	r.ModifyPlan(context.Background(), req, &resp)

	requireNoErrors(t, resp.Diagnostics)
	requireAttributeWarning(t, resp.Diagnostics, "name")
}

func TestBoardModifyPlanIgnoresDestroy(t *testing.T) {
	fake := newFakeHomarr()
	r := &boardResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	null := tfsdk.Plan{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
	req := resource.ModifyPlanRequest{Plan: null, State: stateOf(t, s, boardStateModel("infra", true))}
	resp := resource.ModifyPlanResponse{Plan: null}
	r.ModifyPlan(context.Background(), req, &resp)

	requireNoErrors(t, resp.Diagnostics)
	if fake.called("GET /api/boards") {
		t.Fatal("listed boards while planning a destroy")
	}
}

// --- import by name -----------------------------------------------------------

func importBoard(t *testing.T, r resource.ResourceWithImportState, id string) resource.ImportStateResponse {
	t.Helper()
	resp := resource.ImportStateResponse{State: nullStateFor(t, r)}
	r.ImportState(context.Background(), resource.ImportStateRequest{ID: id}, &resp)
	return resp
}

func TestBoardImportByName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)
	r := &boardResource{client: fake.client(t)}

	resp := importBoard(t, r, "name=Media,12")
	requireNoErrors(t, resp.Diagnostics)

	var id types.String
	var columns types.Int64
	requireNoErrors(t, resp.State.GetAttribute(context.Background(), path.Root("id"), &id))
	requireNoErrors(t, resp.State.GetAttribute(context.Background(), path.Root("column_count"), &columns))
	if id.ValueString() != "b2" || columns.ValueInt64() != 12 {
		t.Fatalf("imported id = %v, column_count = %v; want b2 and 12", id, columns)
	}
}

func TestBoardImportByAmbiguousName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[`+
		`{"id":"b1","name":"infra","isPublic":true,"isHome":false,"isMobileHome":false},`+
		`{"id":"b2","name":"Infra","isPublic":true,"isHome":false,"isMobileHome":false}]`)
	r := &boardResource{client: fake.client(t)}

	resp := importBoard(t, r, "name=INFRA,12")
	requireErrorContaining(t, resp.Diagnostics, "Ambiguous board name")

	// The exact spelling still resolves.
	resp = importBoard(t, r, "name=Infra,12")
	requireNoErrors(t, resp.Diagnostics)
}

func TestBoardImportByUnknownName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsInfraAndMedia)
	r := &boardResource{client: fake.client(t)}

	resp := importBoard(t, r, "name=nope,12")
	requireErrorContaining(t, resp.Diagnostics, `No board named "nope"`)
}

func TestBoardImportByIDNeedsNoLookup(t *testing.T) {
	fake := newFakeHomarr()
	fake.handle("GET /api/boards", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{}`)
	})
	r := &boardResource{client: fake.client(t)}

	resp := importBoard(t, r, "b9,10")
	requireNoErrors(t, resp.Diagnostics)
	if fake.called("GET /api/boards") {
		t.Fatal("an import by id looked the board up")
	}
}
