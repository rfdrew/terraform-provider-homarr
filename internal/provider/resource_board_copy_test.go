package provider

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	helper "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccBoardCopyResource(t *testing.T) {
	helper.Test(t, helper.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []helper.TestStep{
			{
				// The template is public; the copy inherits that unless it says
				// otherwise, and is never made the creator's home board.
				Config: providerConfig + `
resource "homarr_board" "template" {
  name         = "tfacccopytemplate"
  column_count = 8
  is_public    = true
}

resource "homarr_board_copy" "inherits" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopyinherits"
}

resource "homarr_board_copy" "private" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopyprivate"
  is_public       = false
}
`,
				Check: helper.ComposeAggregateTestCheckFunc(
					helper.TestCheckResourceAttrSet("homarr_board_copy.inherits", "id"),
					helper.TestCheckResourceAttrSet("homarr_board_copy.inherits", "creator_id"),
					helper.TestCheckResourceAttr("homarr_board_copy.inherits", "is_public", "true"),
					helper.TestCheckResourceAttr("homarr_board_copy.inherits", "is_mobile_home", "false"),
					helper.TestCheckResourceAttr("homarr_board_copy.private", "is_public", "false"),
					helper.TestCheckResourceAttrPair(
						"homarr_board_copy.private", "source_board_id",
						"homarr_board.template", "id",
					),
				),
			},
			{
				// Renaming a copy happens in place, through the same endpoint as
				// homarr_board, and keeps its id.
				Config: providerConfig + `
resource "homarr_board" "template" {
  name         = "tfacccopytemplate"
  column_count = 8
  is_public    = true
}

resource "homarr_board_copy" "inherits" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopyrenamed"
}

resource "homarr_board_copy" "private" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopyprivate"
  is_public       = false
}
`,
				Check: helper.ComposeAggregateTestCheckFunc(
					helper.TestCheckResourceAttr("homarr_board_copy.inherits", "name", "tfacccopyrenamed"),
					helper.TestCheckResourceAttr("homarr_board_copy.inherits", "is_public", "true"),
				),
			},
			{
				// The source cannot be read back, so an import leaves it out.
				ResourceName:            "homarr_board_copy.private",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"source_board_id"},
			},
			{
				// Copies can also be imported by name, case-insensitively.
				ResourceName:            "homarr_board_copy.private",
				ImportState:             true,
				ImportStateId:           "name=TFACCCOPYPRIVATE",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"source_board_id"},
			},
		},
	})
}

func TestAccBoardCopyResource_takenName(t *testing.T) {
	helper.Test(t, helper.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []helper.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "template" {
  name         = "tfacccopytaken"
  column_count = 10
}
`,
			},
			{
				// Homarr refuses a copy under a name differing only in case
				// with 409; the plan only warns about it.
				Config: providerConfig + `
resource "homarr_board" "template" {
  name         = "tfacccopytaken"
  column_count = 10
}

resource "homarr_board_copy" "clash" {
  source_board_id = homarr_board.template.id
  name            = "TFACCCOPYTAKEN"
}
`,
				ExpectError: regexpMustCompile(`Board name already taken`),
			},
		},
	})
}

// --- unit tests -----------------------------------------------------------------

func copyPlan(isPublic types.Bool) *boardCopyModel {
	return &boardCopyModel{
		ID:            types.StringUnknown(),
		SourceBoardID: types.StringValue("template"),
		Name:          types.StringValue("team-a"),
		IsPublic:      isPublic,
		LogoImageURL:  types.StringUnknown(),
		CreatorID:     types.StringUnknown(),
		IsHome:        types.BoolUnknown(),
		IsMobileHome:  types.BoolUnknown(),
	}
}

func createCopy(t *testing.T, r *boardCopyResource, plan *boardCopyModel) resource.CreateResponse {
	t.Helper()
	s := resourceSchema(t, r)
	resp := resource.CreateResponse{State: nullStateFor(t, r)}
	r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, s, plan)}, &resp)
	return resp
}

const listedCopy = `[{"id":"c1","name":"team-a","isPublic":false,"creator":{"id":"u1","name":"admin","image":null},` +
	`"isHome":false,"isMobileHome":false}]`

func TestBoardCopyCreateSetsAConfiguredVisibility(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
	fake.reply("PATCH /api/boards/c1/visibility", 200, ``)
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	resp := createCopy(t, r, copyPlan(types.BoolValue(false)))
	requireNoErrors(t, resp.Diagnostics)

	if body := fake.body(t, "PATCH /api/boards/c1/visibility"); body["visibility"] != "private" {
		t.Fatalf("visibility body = %v, want private", body)
	}
	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "c1" || got.SourceBoardID.ValueString() != "template" || got.CreatorID.ValueString() != "u1" {
		t.Fatalf("state = %+v", got)
	}
}

func TestBoardCopyCreateInheritsAnOmittedVisibility(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	resp := createCopy(t, r, copyPlan(types.BoolUnknown()))
	requireNoErrors(t, resp.Diagnostics)

	if fake.called("PATCH /api/boards/c1/visibility") {
		t.Fatal("changed the visibility although the configuration left it out")
	}
	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.IsPublic.IsUnknown() || got.IsPublic.ValueBool() {
		t.Fatalf("is_public = %v, want the inherited false", got.IsPublic)
	}
}

func TestBoardCopyCreateTaintsRatherThanLeaksOnAFollowUpFailure(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
	fake.reply("PATCH /api/boards/c1/visibility", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	resp := createCopy(t, r, copyPlan(types.BoolValue(false)))

	requireErrorContaining(t, resp.Diagnostics, "boom")
	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "c1" {
		t.Fatalf("state id = %v, want the copy kept in state", got.ID)
	}
}

// A copy inherits the source's visibility. When making a copy of a public
// board private fails, the error alone does not say that the copy is public.
func TestBoardCopyCreateWarnsThatAFailedVisibilityChangeLeftItPublic(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
	fake.reply("PATCH /api/boards/c1/visibility", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("GET /api/boards", 200, strings.Replace(listedCopy, `"isPublic":false`, `"isPublic":true`, 1))
	r := &boardCopyResource{client: fake.client(t)}

	resp := createCopy(t, r, copyPlan(types.BoolValue(false)))

	requireErrorContaining(t, resp.Diagnostics, "boom")
	warned := false
	for _, d := range resp.Diagnostics.Warnings() {
		withPath, ok := d.(diag.DiagnosticWithPath)
		if ok && withPath.Path().Equal(path.Root("is_public")) && strings.Contains(d.Detail(), "is public") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no warning on is_public that the copy is public: %v", resp.Diagnostics)
	}
	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if !got.IsPublic.ValueBool() {
		t.Fatalf("is_public = %v, want the inherited true recorded", got.IsPublic)
	}
}

func TestBoardCopyCreateDoesNotWarnAboutAPrivateCopy(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
	fake.reply("PATCH /api/boards/c1/visibility", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`)
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	resp := createCopy(t, r, copyPlan(types.BoolValue(false)))
	if n := resp.Diagnostics.WarningsCount(); n != 0 {
		t.Fatalf("warned %d times about a copy that read back private: %v", n, resp.Diagnostics)
	}
}

func TestBoardCopyCreateExplainsRefusals(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      string
		attribute string
	}{
		{"taken name", 409, `{"message":"Board with similar name already exists","code":"CONFLICT"}`, "Board name already taken", "name"},
		{"missing or invisible source", 404, boardNotFoundBody, "lacks view access", "source_board_id"},
		{"unusable integration", 404, `{"message":"Integration not found","code":"NOT_FOUND"}`, "may not use", ""},
		{"widget without its integration", 400, `{"message":"calendar requires an integration","code":"BAD_REQUEST"}`, "requires an integration but has none", ""},
		{"custom widgets", 403, `{"message":"Admin permission required","code":"FORBIDDEN"}`, "custom widgets", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("POST /api/boards/template/duplicate", tc.status, tc.body)
			r := &boardCopyResource{client: fake.client(t)}

			resp := createCopy(t, r, copyPlan(types.BoolUnknown()))

			requireErrorContaining(t, resp.Diagnostics, tc.want)
			if tc.attribute != "" {
				requireAttributeError(t, resp.Diagnostics, tc.attribute)
			}
			if !resp.State.Raw.IsNull() {
				t.Fatal("a refused copy was written to state")
			}
		})
	}
}

func copyState(source types.String) *boardCopyModel {
	return &boardCopyModel{
		ID:            types.StringValue("c1"),
		SourceBoardID: source,
		Name:          types.StringValue("team-a"),
		IsPublic:      types.BoolValue(false),
		LogoImageURL:  types.StringNull(),
		CreatorID:     types.StringValue("u1"),
		IsHome:        types.BoolValue(false),
		IsMobileHome:  types.BoolValue(false),
	}
}

// After an import the first apply only records source_board_id.
func TestBoardCopyUpdateRecordsTheSourceAfterImport(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	resp := resource.UpdateResponse{State: nullStateFor(t, r)}
	r.Update(context.Background(), resource.UpdateRequest{
		Plan:  planOf(t, s, copyState(types.StringValue("template"))),
		State: stateOf(t, s, copyState(types.StringNull())),
	}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	for _, route := range []string{"PATCH /api/boards/c1/name", "PATCH /api/boards/c1/visibility", "POST /api/boards/template/duplicate"} {
		if fake.called(route) {
			t.Fatalf("recording the source called %s", route)
		}
	}
	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.SourceBoardID.ValueString() != "template" {
		t.Fatalf("source_board_id = %v, want it recorded", got.SourceBoardID)
	}
}

func TestBoardCopyDeleteWithoutFullAccessKeepsTheCopy(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("DELETE /api/boards/c1", 404, boardNotFoundBody)
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	state := stateOf(t, resourceSchema(t, r), copyState(types.StringValue("template")))
	resp := resource.DeleteResponse{State: state}
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

	requireErrorContaining(t, resp.Diagnostics, "deleting a board requires full access")
}

func TestBoardCopyImportByName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, listedCopy)
	r := &boardCopyResource{client: fake.client(t)}

	resp := importBoard(t, r, "name=Team-A")
	requireNoErrors(t, resp.Diagnostics)

	var got boardCopyModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "c1" || !got.SourceBoardID.IsNull() {
		t.Fatalf("imported state = %+v, want id c1 and no source", got)
	}
}

// checkCopyIDDiffers asserts that a replaced copy came back under a new id.
func checkCopyIDDiffers(resourceName string, previous *string) helper.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		if *previous != "" && rs.Primary.ID == *previous {
			return fmt.Errorf("%s kept id %s although it should have been replaced", resourceName, rs.Primary.ID)
		}
		*previous = rs.Primary.ID
		return nil
	}
}

// Replacing the source replaces its copies; ignore_changes keeps them.
func TestAccBoardCopyResource_sourceReplacement(t *testing.T) {
	var replacedID, keptID string
	helper.Test(t, helper.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []helper.TestStep{
			{
				Config: providerConfig + testAccBoardCopySourceConfig(10),
				Check: helper.ComposeAggregateTestCheckFunc(
					checkCopyIDDiffers("homarr_board_copy.replaced", &replacedID),
					checkCopyIDDiffers("homarr_board_copy.kept", &keptID),
				),
			},
			{
				// A new column count replaces the template, and with it every
				// copy that tracks its id.
				Config: providerConfig + testAccBoardCopySourceConfig(12),
				Check: helper.ComposeAggregateTestCheckFunc(
					checkCopyIDDiffers("homarr_board_copy.replaced", &replacedID),
					helper.TestCheckResourceAttrPtr("homarr_board_copy.kept", "id", &keptID),
				),
			},
		},
	})
}

func testAccBoardCopySourceConfig(columns int) string {
	return fmt.Sprintf(`
resource "homarr_board" "template" {
  name         = "tfacccopysource"
  column_count = %d
}

resource "homarr_board_copy" "replaced" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopyreplaced"
}

resource "homarr_board_copy" "kept" {
  source_board_id = homarr_board.template.id
  name            = "tfacccopykept"

  lifecycle {
    ignore_changes = [source_board_id]
  }
}
`, columns)
}
