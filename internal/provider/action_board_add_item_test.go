package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	helper "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// Actions arrived in Terraform 1.14. The action keeps no state and Homarr
// cannot read tiles back, so success of the apply is all there is to check:
// the add, and the move to a position in the board's base layout.
func TestAccBoardAddItemAction(t *testing.T) {
	helper.Test(t, helper.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []helper.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfaccadditem"
  column_count = 10
}

resource "homarr_app" "test" {
  name     = "tfaccadditem"
  icon_url = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/homarr.svg"

  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.homarr_board_add_item.app, action.homarr_board_add_item.clock]
    }
  }
}

action "homarr_board_add_item" "app" {
  config {
    board_id     = homarr_board.test.id
    kind         = "app"
    options_json = jsonencode({ appId = homarr_app.test.id })
    width        = 2
    height       = 1

    # Moved off the automatic position, well clear of the clock tile.
    layout_id = homarr_board.test.base_layout_id
    x         = 8
    y         = 6
  }
}

action "homarr_board_add_item" "clock" {
  config {
    board_id = homarr_board.test.id
    kind     = "clock"
  }
}
`,
			},
		},
	})
}

func addItemConfig(t *testing.T, attrs map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	var resp action.SchemaResponse
	(&boardAddItemAction{}).Schema(context.Background(), action.SchemaRequest{}, &resp)
	typ := resp.Schema.Type().TerraformType(context.Background()).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range typ.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = v
		} else {
			values[name] = tftypes.NewValue(attrType, nil)
		}
	}
	return tfsdk.Config{Schema: resp.Schema, Raw: tftypes.NewValue(typ, values)}
}

func invokeAddItem(t *testing.T, a *boardAddItemAction, attrs map[string]tftypes.Value) action.InvokeResponse {
	t.Helper()
	var resp action.InvokeResponse
	a.Invoke(context.Background(), action.InvokeRequest{Config: addItemConfig(t, attrs)}, &resp)
	return resp
}

func TestBoardAddItemSendsDefaultsForAnUnsizedWidget(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/items", 200, `{"itemId":"i1"}`)
	a := &boardAddItemAction{client: fake.client(t)}

	resp := invokeAddItem(t, a, map[string]tftypes.Value{"board_id": tfString("b1"), "kind": tfString("clock")})
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "POST /api/boards/items")
	if _, ok := body["size"]; ok {
		t.Fatalf("size = %v, want it left to the widget's default", body["size"])
	}
	if options, ok := body["options"].(map[string]any); !ok || len(options) != 0 {
		t.Fatalf("options = %#v, want {}", body["options"])
	}
}

func TestBoardAddItemExplainsRefusals(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      string
		attribute string
	}{
		{"missing or unmodifiable board", 404, boardNotFoundBody, "lacks modify access", "board_id"},
		{"unusable integration", 404, `{"message":"Integration not found","code":"NOT_FOUND"}`, "may not use", "integration_ids"},
		{"widget without its integration", 400, `{"message":"calendar requires an integration","code":"BAD_REQUEST"}`, "some kinds require one", "integration_ids"},
		{"full board", 400, `{"message":"Board section is full, no free grid position available","code":"BAD_REQUEST"}`, "Board section is full", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("POST /api/boards/items", tc.status, tc.body)
			a := &boardAddItemAction{client: fake.client(t)}

			resp := invokeAddItem(t, a, map[string]tftypes.Value{"board_id": tfString("b1"), "kind": tfString("calendar")})

			requireErrorContaining(t, resp.Diagnostics, tc.want)
			if tc.attribute != "" {
				requireAttributeError(t, resp.Diagnostics, tc.attribute)
			}
		})
	}
}

func positionedTile(layoutID tftypes.Value) map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2), "height": tfNumber(1),
		"layout_id": layoutID, "x": tfNumber(4), "y": tfNumber(0),
	}
}

func TestBoardAddItemMovesTheTileToTheGivenPosition(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/items", 200, `{"itemId":"i1"}`)
	fake.reply("PATCH /api/boards/b1/items/i1/layouts/l1", 200,
		`{"boardId":"b1","itemId":"i1","layoutId":"l1","xOffset":4,"yOffset":0,"width":2,"height":1}`)
	a := &boardAddItemAction{client: fake.client(t)}

	var progress []string
	var resp action.InvokeResponse
	resp.SendProgress = func(e action.InvokeProgressEvent) { progress = append(progress, e.Message) }
	a.Invoke(context.Background(), action.InvokeRequest{Config: addItemConfig(t, positionedTile(tfString("l1")))}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PATCH /api/boards/b1/items/i1/layouts/l1")
	if body["xOffset"] != float64(4) || body["yOffset"] != float64(0) || body["width"] != float64(2) || body["height"] != float64(1) {
		t.Fatalf("move body = %v, want the configured position and size", body)
	}
	if len(progress) != 2 {
		t.Fatalf("progress = %v, want one message for the add and one for the move", progress)
	}
}

func TestBoardAddItemWithoutAPositionDoesNotMoveTheTile(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/items", 200, `{"itemId":"i1"}`)
	a := &boardAddItemAction{client: fake.client(t)}

	resp := invokeAddItem(t, a, map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2), "height": tfNumber(1),
	})
	requireNoErrors(t, resp.Diagnostics)
	if fake.called("PATCH /api/boards/b1/items/i1/layouts/l1") {
		t.Fatal("moved a tile although no position was configured")
	}
}

// base_layout_id is null for imported boards; finding that out only after the
// tile was added would leave a tile behind for nothing.
func TestBoardAddItemWithAPositionButNoLayoutAddsNothing(t *testing.T) {
	fake := newFakeHomarr()
	a := &boardAddItemAction{client: fake.client(t)}

	resp := invokeAddItem(t, a, positionedTile(tftypes.NewValue(tftypes.String, nil)))

	requireAttributeError(t, resp.Diagnostics, "layout_id")
	requireErrorContaining(t, resp.Diagnostics, "imported")
	if fake.called("POST /api/boards/items") {
		t.Fatal("added a tile that could not be moved to its position")
	}
}

// When the move fails, the tile already exists at the automatic position. The
// error has to say so, because invoking the action again adds a second one.
func TestBoardAddItemReportsAFailedMoveWithTheAddedTile(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      string
		attribute string
	}{
		{"overlap", 409, `{"message":"Item layout overlaps another item or section","code":"CONFLICT"}`, "already covers", "x"},
		{"out of bounds", 400, `{"message":"Item layout exceeds its section bounds","code":"BAD_REQUEST"}`, "x + width", "x"},
		{"foreign layout", 404, `{"message":"Item layout not found on this board","code":"NOT_FOUND"}`, "not a layout of this board", "layout_id"},
		{"server error", 500, `{"message":"boom","code":"INTERNAL_SERVER_ERROR"}`, "boom", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("POST /api/boards/items", 200, `{"itemId":"i1"}`)
			fake.reply("PATCH /api/boards/b1/items/i1/layouts/l1", tc.status, tc.body)
			a := &boardAddItemAction{client: fake.client(t)}

			resp := invokeAddItem(t, a, positionedTile(tfString("l1")))

			requireErrorContaining(t, resp.Diagnostics, tc.want)
			requireErrorContaining(t, resp.Diagnostics, "added as item i1")
			requireErrorContaining(t, resp.Diagnostics, "adds another tile")
			if tc.attribute != "" {
				requireAttributeError(t, resp.Diagnostics, tc.attribute)
			}
		})
	}
}

func TestBoardAddItemWithoutAConfiguredProvider(t *testing.T) {
	a := &boardAddItemAction{}

	resp := invokeAddItem(t, a, map[string]tftypes.Value{"board_id": tfString("b1"), "kind": tfString("clock")})
	requireErrorContaining(t, resp.Diagnostics, "Unconfigured Homarr provider")
}

func TestJSONObjectValidator(t *testing.T) {
	tests := map[string]bool{
		`{}`:                 true,
		`{"appId":"a1"}`:     true,
		`{"nested":{"a":1}}`: true,
		`null`:               false,
		`[]`:                 false,
		`"text"`:             false,
		`{"broken":`:         false,
	}
	for value, valid := range tests {
		var resp validator.StringResponse
		jsonObjectValidator().ValidateString(context.Background(), validator.StringRequest{
			ConfigValue: types.StringValue(value),
		}, &resp)
		if got := !resp.Diagnostics.HasError(); got != valid {
			t.Errorf("options_json %s: valid = %v, want %v", value, got, valid)
		}
	}
}
