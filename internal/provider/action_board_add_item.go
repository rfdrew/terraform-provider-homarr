package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/actionvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/action/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ action.Action                     = &boardAddItemAction{}
	_ action.ActionWithConfigure        = &boardAddItemAction{}
	_ action.ActionWithConfigValidators = &boardAddItemAction{}
)

// NewBoardAddItemAction returns the homarr_board_add_item action.
//
// Board tiles cannot be a managed resource: no REST call reads one back or
// deletes a single one. Placing a tile is still a useful one-off side effect,
// which is what Terraform actions (1.14+) model: they keep no state, and run
// only when a lifecycle trigger or `terraform apply -invoke` says so.
func NewBoardAddItemAction() action.Action { return &boardAddItemAction{} }

type boardAddItemAction struct {
	client *client.Client
}

type boardAddItemModel struct {
	BoardID        types.String `tfsdk:"board_id"`
	Kind           types.String `tfsdk:"kind"`
	OptionsJSON    types.String `tfsdk:"options_json"`
	IntegrationIDs types.List   `tfsdk:"integration_ids"`
	Width          types.Int64  `tfsdk:"width"`
	Height         types.Int64  `tfsdk:"height"`
	LayoutID       types.String `tfsdk:"layout_id"`
	X              types.Int64  `tfsdk:"x"`
	Y              types.Int64  `tfsdk:"y"`
}

func (a *boardAddItemAction) Metadata(_ context.Context, req action.MetadataRequest, resp *action.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board_add_item"
}

func (a *boardAddItemAction) Schema(_ context.Context, _ action.SchemaRequest, resp *action.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Adds a widget or app tile to a Homarr board, through `POST /api/boards/items`.\n\n" +
			"This is a Terraform *action*, not a resource, and needs **Terraform 1.14 or newer**. It runs only " +
			"when invoked — from a resource's `lifecycle { action_trigger { ... } }` or with " +
			"`terraform apply -invoke=action.homarr_board_add_item.<name>` — and remembers nothing afterwards. " +
			"Homarr places the tile at the first free cell of the board's main section, in every layout " +
			"including the mobile one, and returns only the new tile's id, which the action logs. With " +
			"`layout_id`, `x` and `y` the action then moves the tile to that position in that one layout — in " +
			"practice the board's desktop layout, through `homarr_board`'s `base_layout_id` — and the other " +
			"layouts keep the automatic position.\n\n" +
			"~> **Not idempotent.** Every invocation adds another tile. Trigger it on `after_create` of the " +
			"resource the tile belongs to, never on `after_update`, and expect a duplicate tile from every " +
			"`-invoke`.\n\n" +
			"~> **Terraform cannot see, change or remove the tile afterwards.** No REST call reads a board's " +
			"tiles or deletes a single one, so a tile added here is not in state, is not removed on destroy, " +
			"and must be edited or deleted in the Homarr UI. Destroying the board removes its tiles with it.\n\n" +
			"Further caveats, all imposed by Homarr's API:\n\n" +
			"* App tiles reference the app by id, in `options_json = jsonencode({ appId = homarr_app.x.id })`. " +
			"Homarr does not clean tiles up when an app is deleted, so destroying or replacing the " +
			"`homarr_app` leaves a tile pointing at nothing.\n" +
			"* Widgets that need an integration are refused without `integration_ids` " +
			"(`<kind> requires an integration`), and integrations have no REST API to list them: the id has " +
			"to be copied from the address bar of the integration's edit page in the UI. In practice the " +
			"action suits integration-free kinds such as `app`, `bookmarks`, `notebook`, `clock` and `iframe`.\n" +
			"* Homarr stores `options_json` as given and validates hardly any of it, so a misspelt option " +
			"silently yields a tile with that option at its default. Option names belong to each widget and " +
			"change between Homarr releases.\n" +
			"* The API key needs modify access to the board, use access to every listed integration, and an " +
			"admin for custom widgets. A board without a free cell is refused (`Board section is full`).",
		Attributes: map[string]schema.Attribute{
			"board_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the board to add the tile to.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"kind": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Widget kind, as Homarr names it: for example `app`, `bookmarks`, `notebook`, " +
					"`clock`, `iframe` or `weather`. The set of kinds grows with each Homarr release, so it is " +
					"checked by Homarr rather than here.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"options_json": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The widget's options as a JSON object, typically built with `jsonencode`. " +
					"Options left out take the widget's defaults. Defaults to `{}`.",
				Validators: []validator.String{
					jsonObjectValidator(),
				},
			},
			"integration_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				MarkdownDescription: "Identifiers of the integrations the widget reads from, at most 32. Required " +
					"by Homarr for widgets that only work with an integration.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(32),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"width": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Width of the tile in grid columns, between 1 and 24. Homarr narrows it to " +
					"fit each layout, such as the 3-column mobile layout. Set together with `height`; " +
					"leave both out for the widget's default size.",
				Validators: []validator.Int64{
					int64validator.Between(1, 24),
				},
			},
			"height": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Height of the tile in grid rows, between 1 and 24. Set together with `width`.",
				Validators: []validator.Int64{
					int64validator.Between(1, 24),
				},
			},
			"layout_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Identifier of the layout in which to move the tile to `x` and `y`, through " +
					"`PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}`. Set it to " +
					"`homarr_board.<name>.base_layout_id`, the board's desktop layout: that is the only layout id " +
					"any REST response contains. It is null for a board that was imported or created before " +
					"provider 0.3.0, and `homarr_board_copy` has none at all. Tiles on such boards can only take " +
					"Homarr's automatic position: leave `layout_id`, `x` and `y` out for them, since the action " +
					"fails when `x` and `y` are set while `layout_id` is null. Set together with `x` and `y`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"x": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Column of the tile's left edge in `layout_id`, counted from 0. The tile has to " +
					"fit: `x + width` must not exceed the columns of the board's main area — `column_count` for a " +
					"board made by `homarr_board`, less any side columns set up in the UI. Homarr refuses a " +
					"position that overlaps another tile. Needs `layout_id`, `y`, `width` and `height`.",
				Validators: []validator.Int64{
					int64validator.Between(0, 32767),
					int64validator.AlsoRequires(path.MatchRoot("width"), path.MatchRoot("height")),
				},
			},
			"y": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Row of the tile's top edge in `layout_id`, counted from 0. Set together with `x`.",
				Validators: []validator.Int64{
					int64validator.Between(0, 32767),
				},
			},
		},
	}
}

func (a *boardAddItemAction) ConfigValidators(_ context.Context) []action.ConfigValidator {
	// Homarr's size object needs both dimensions or neither, and moving a tile
	// needs the whole placement: a layout, both offsets and the size.
	return []action.ConfigValidator{
		actionvalidator.RequiredTogether(path.MatchRoot("width"), path.MatchRoot("height")),
		actionvalidator.RequiredTogether(path.MatchRoot("layout_id"), path.MatchRoot("x"), path.MatchRoot("y")),
	}
}

func (a *boardAddItemAction) Configure(_ context.Context, req action.ConfigureRequest, resp *action.ConfigureResponse) {
	a.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// jsonObjectValidator accepts a string holding a JSON object, the only shape
// Homarr's options record takes.
func jsonObjectValidator() validator.String {
	return stringCheck{
		description: "must be a JSON object, for example jsonencode({ appId = homarr_app.example.id })",
		check: func(s string) bool {
			var object map[string]json.RawMessage
			return json.Unmarshal([]byte(s), &object) == nil && object != nil
		},
	}
}

// request turns the configuration into the POST /api/boards/items body.
func (m boardAddItemModel) request(ctx context.Context) (client.BoardItemCreateRequest, diag.Diagnostics) {
	req := client.BoardItemCreateRequest{
		BoardID: m.BoardID.ValueString(),
		Kind:    m.Kind.ValueString(),
	}
	if !m.OptionsJSON.IsNull() {
		req.Options = json.RawMessage(m.OptionsJSON.ValueString())
	}
	var diags diag.Diagnostics
	if !m.IntegrationIDs.IsNull() {
		diags.Append(m.IntegrationIDs.ElementsAs(ctx, &req.IntegrationIDs, false)...)
	}
	if !m.Width.IsNull() && !m.Height.IsNull() {
		req.Size = &client.BoardItemSize{Width: m.Width.ValueInt64(), Height: m.Height.ValueInt64()}
	}
	return req, diags
}

// placement returns where the configuration asks the tile to be moved, or nil
// when it leaves the tile where Homarr puts it.
//
// The config validators require the placement attributes together, but
// layout_id usually comes from homarr_board.base_layout_id, which is unknown
// while validating a plan that creates the board and can turn out null. That
// is caught here, before the tile is added.
func (m boardAddItemModel) placement() (*client.BoardItemLayout, diag.Diagnostics) {
	var diags diag.Diagnostics
	if m.X.IsNull() && m.Y.IsNull() {
		return nil, diags
	}
	if m.LayoutID.IsNull() || m.LayoutID.ValueString() == "" {
		diags.AddAttributeError(
			path.Root("layout_id"),
			"Missing layout for the tile's position",
			"x and y are set, but layout_id is null, so the tile could not be moved there. "+
				"homarr_board's base_layout_id is null for a board that was imported or created before provider "+
				"0.3.0, and homarr_board_copy has none: Homarr returns a layout id only when it creates a board. "+
				"Leave out layout_id, x and y to keep Homarr's automatic position.",
		)
		return nil, diags
	}
	if m.X.IsNull() || m.Y.IsNull() || m.Width.IsNull() || m.Height.IsNull() {
		diags.AddAttributeError(
			path.Root("x"),
			"Incomplete tile position",
			"Moving a tile needs layout_id, x, y, width and height together.",
		)
		return nil, diags
	}
	return &client.BoardItemLayout{
		XOffset: m.X.ValueInt64(),
		YOffset: m.Y.ValueInt64(),
		Width:   m.Width.ValueInt64(),
		Height:  m.Height.ValueInt64(),
	}, diags
}

func (a *boardAddItemAction) Invoke(ctx context.Context, req action.InvokeRequest, resp *action.InvokeResponse) {
	var config boardAddItemModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if a.client == nil {
		resp.Diagnostics.AddError(
			"Unconfigured Homarr provider",
			"The action ran without a configured provider. This is a bug in the provider.",
		)
		return
	}

	item, diags := config.request(ctx)
	resp.Diagnostics.Append(diags...)
	position, diags := config.placement()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	itemID, err := a.client.AddBoardItem(ctx, item)
	if err != nil {
		reportAddItemError(item.BoardID, err, &resp.Diagnostics)
		return
	}

	tflog.Info(ctx, "added a tile to a Homarr board", map[string]any{
		"board_id": item.BoardID,
		"kind":     item.Kind,
		"item_id":  itemID,
	})
	sendAddItemProgress(resp, fmt.Sprintf("Added %s tile %s to board %s", item.Kind, itemID, item.BoardID))
	if position == nil {
		return
	}

	// POST /api/boards/items takes no position, so the tile is moved right
	// after it was added. The tile exists either way.
	layoutID := config.LayoutID.ValueString()
	if err := a.client.MoveBoardItem(ctx, item.BoardID, itemID, layoutID, *position); err != nil {
		reportMoveItemError(itemID, layoutID, err, &resp.Diagnostics)
		return
	}
	tflog.Info(ctx, "moved the new tile", map[string]any{
		"item_id":   itemID,
		"layout_id": layoutID,
		"x":         position.XOffset,
		"y":         position.YOffset,
	})
	sendAddItemProgress(resp, fmt.Sprintf("Moved tile %s to column %d, row %d of layout %s",
		itemID, position.XOffset, position.YOffset, layoutID))
}

// sendAddItemProgress reports a step of the invocation to Terraform's UI.
func sendAddItemProgress(resp *action.InvokeResponse, message string) {
	if resp.SendProgress != nil {
		resp.SendProgress(action.InvokeProgressEvent{Message: message})
	}
}

// reportAddItemError explains a failed POST /api/boards/items. As with board
// copies, Homarr uses 404 for several refusals, told apart by its message.
func reportAddItemError(boardID string, err error, diags *diag.Diagnostics) {
	apiErr := homarrError(err)
	status, message := 0, ""
	if apiErr != nil {
		status, message = apiErr.StatusCode, apiErr.Message
	}

	switch {
	case status == http.StatusNotFound && strings.Contains(message, "Integration"):
		diags.AddAttributeError(
			path.Root("integration_ids"),
			"Integration not found",
			"Homarr answers 404 both for an integration that does not exist and for one the API key's user "+
				"may not use. "+err.Error(),
		)
	case status == http.StatusNotFound:
		diags.AddAttributeError(
			path.Root("board_id"),
			"Board not found",
			"Homarr has no board "+boardID+" that this API key can modify. It answers 404 both for a missing "+
				"board and for one the key lacks modify access to. "+err.Error(),
		)
	case status == http.StatusBadRequest && strings.Contains(message, "integration"):
		diags.AddAttributeError(
			path.Root("integration_ids"),
			"Unable to add tile to Homarr board",
			"Homarr checks the integrations against the widget kind: some kinds require one, others take "+
				"none or only certain integration kinds. "+err.Error(),
		)
	default:
		diags.AddError("Unable to add tile to Homarr board", err.Error())
	}
}

// reportMoveItemError explains a failed move of a tile the action has just
// added. The tile exists at Homarr's automatic position, and running the
// action again would add a second one, so every message says so.
func reportMoveItemError(itemID, layoutID string, err error, diags *diag.Diagnostics) {
	apiErr := homarrError(err)
	status, message := 0, ""
	if apiErr != nil {
		status, message = apiErr.StatusCode, apiErr.Message
	}

	const summary = "Unable to move the new tile on the Homarr board"
	added := "The tile was added as item " + itemID + " at the first free cell; only moving it failed, so it " +
		"stays there. Move or delete it in the Homarr UI: invoking the action again adds another tile. "

	switch {
	case status == http.StatusNotFound && strings.Contains(message, "layout"):
		diags.AddAttributeError(
			path.Root("layout_id"),
			summary,
			added+"Layout "+layoutID+" is not a layout of this board; use the base_layout_id of the board "+
				"named in board_id. "+err.Error(),
		)
	case status == http.StatusBadRequest && strings.Contains(message, "bounds"):
		diags.AddAttributeError(
			path.Root("x"),
			summary,
			added+"The tile does not fit there: x + width must not exceed the columns of the board's main "+
				"area. "+err.Error(),
		)
	case status == http.StatusConflict:
		diags.AddAttributeError(
			path.Root("x"),
			summary,
			added+"Another tile or section already covers part of that position. "+err.Error(),
		)
	default:
		diags.AddError(summary, added+err.Error())
	}
}
