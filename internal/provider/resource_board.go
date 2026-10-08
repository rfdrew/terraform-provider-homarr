package provider

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// boardNamePattern mirrors boardNameSchema in Homarr: names are restricted to
// ASCII letters, digits, hyphens and underscores.
var boardNamePattern = regexp.MustCompile(`^[A-Za-z0-9-_]+$`)

var (
	_ resource.Resource                = &boardResource{}
	_ resource.ResourceWithConfigure   = &boardResource{}
	_ resource.ResourceWithImportState = &boardResource{}
)

// NewBoardResource returns the homarr_board resource.
func NewBoardResource() resource.Resource { return &boardResource{} }

type boardResource struct {
	client *client.Client
}

type boardModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	ColumnCount  types.Int64  `tfsdk:"column_count"`
	IsPublic     types.Bool   `tfsdk:"is_public"`
	LogoImageURL types.String `tfsdk:"logo_image_url"`
	CreatorID    types.String `tfsdk:"creator_id"`
	IsHome       types.Bool   `tfsdk:"is_home"`
	IsMobileHome types.Bool   `tfsdk:"is_mobile_home"`
}

func (r *boardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board"
}

func (r *boardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr board — a dashboard page.\n\n" +
			"A new board is created with one empty section and one layout, exactly as the Homarr UI does. " +
			"The widgets and app tiles placed *on* a board are not manageable here. Homarr can create an item " +
			"over REST (`POST /api/boards/items`) but offers no way to list, update, move, resize or delete one, " +
			"and the board's layout is tRPC-only — not enough to reconcile tiles declaratively.\n\n" +
			"~> **Note** Homarr makes the first board a user creates their home board automatically, so " +
			"`is_home` can come back `true` on a board that never asked for it. `is_home` and " +
			"`is_mobile_home` are the home boards *of the user behind the API key*; the instance-wide " +
			"defaults live in `homarr_server_board_settings`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr-generated identifier of the board.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Unique board name, also used in the board's URL. Only ASCII letters, " +
					"digits, `-` and `_` are allowed; spaces and other punctuation are rejected by Homarr.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(
						boardNamePattern,
						"must contain only ASCII letters, digits, hyphens and underscores",
					),
				},
			},
			"column_count": schema.Int64Attribute{
				Required: true,
				MarkdownDescription: "Number of columns in the board's base layout, between 1 and 24.\n\n" +
					"~> **Note** Homarr's REST API accepts this only at creation — the column count lives on " +
					"the board's layout, which can be changed solely through tRPC. Changing this value " +
					"therefore forces a new board, and the provider cannot detect a column count changed " +
					"through the Homarr UI.",
				Validators: []validator.Int64{
					int64validator.Between(1, 24),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"is_public": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether the board is visible without authentication. Defaults to `false`. " +
					"Homarr refuses to make a board private while it is the *instance-wide* home board — " +
					"the one set through `homarr_server_board_settings`, not the per-user `is_home`.",
			},
			"logo_image_url": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Board logo URL, as reported by Homarr. Set this through " +
					"`homarr_board_settings`, which owns the board's appearance.",
			},
			"creator_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the user that owns the board — the user behind the API key.",
			},
			"is_home": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether this board is the desktop home board of the user behind the " +
					"API key. Leave it out to report whatever Homarr has.\n\n" +
					"The home board is a per-user singleton, so selecting one board deselects whichever " +
					"board held the flag before. Declaring `is_home = true` on two boards makes them " +
					"fight over it on every apply. The board that loses the flag is not re-read during " +
					"that apply, so its state reports `true` until the next refresh.\n\n" +
					"~> **Setting this to `false` requires Homarr 2.3.0 or newer.** Clearing a home board " +
					"goes through `PATCH /api/users/preferences`, which arrived in 2.3.0; before that " +
					"Homarr could only ever select one. Homarr also refuses to make a board private while " +
					"it is the *instance-wide* home board set through `homarr_server_board_settings`.",
			},
			"is_mobile_home": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether this board is the mobile home board of the user behind the " +
					"API key. Behaves exactly like `is_home`, against Homarr's separate mobile slot. " +
					"Unlike `is_home` it is never assigned automatically.",
			},
		},
	}
}

func (r *boardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies the readable board fields into the model.
//
// column_count is deliberately untouched: boardSummarySchema does not carry it,
// so the configured value is the only source of truth Terraform has.
func (m *boardModel) fromAPI(board *client.BoardSummary) {
	m.ID = types.StringValue(board.ID)
	m.Name = types.StringValue(board.Name)
	m.IsPublic = types.BoolValue(board.IsPublic)
	m.LogoImageURL = stringValueOrNull(board.LogoImageURL)
	m.IsHome = types.BoolValue(board.IsHome)
	m.IsMobileHome = types.BoolValue(board.IsMobileHome)
	if board.Creator != nil {
		m.CreatorID = types.StringValue(board.Creator.ID)
	} else {
		m.CreatorID = types.StringNull()
	}
}

func (r *boardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan boardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateBoard(ctx, client.BoardCreateRequest{
		Name:        plan.Name.ValueString(),
		ColumnCount: plan.ColumnCount.ValueInt64(),
		IsPublic:    plan.IsPublic.ValueBool(),
	})
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("name"),
				"Board name already taken",
				"Homarr requires board names to be unique: "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to create Homarr board", err.Error())
		return
	}

	// The home-board flags have no field on POST /api/boards; they are separate
	// calls made against the freshly created board. Their diagnostics are held
	// back so that state is written first: the board exists either way, and
	// returning an error without state would leak it. Reporting the failure
	// afterwards taints the resource instead, which is recoverable.
	var homeDiags diag.Diagnostics
	r.applyHomeFlags(ctx, id, plan, boardModel{}, &homeDiags)

	board, err := r.client.GetBoard(ctx, id)
	if err != nil {
		resp.Diagnostics.Append(homeDiags...)
		resp.Diagnostics.AddError("Unable to read Homarr board back after creation", err.Error())
		return
	}

	plan.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(homeDiags...)
}

// boolSelected reports whether a bool attribute is a known, non-null true —
// that is, whether the configuration actually asked for the flag. An omitted
// Optional+Computed attribute is unknown on create and carries the prior state
// on update, so neither is treated as a request.
func boolSelected(value types.Bool) bool {
	return !value.IsNull() && !value.IsUnknown() && value.ValueBool()
}

// boolCleared is boolSelected's mirror: a known, non-null false, meaning the
// configuration explicitly asked for the flag to be off rather than omitting it.
func boolCleared(value types.Bool) bool {
	return !value.IsNull() && !value.IsUnknown() && !value.ValueBool()
}

// applyHomeFlags selects or clears the board as desktop and/or mobile home
// board, when the plan differs from the prior state.
//
// Selecting uses PATCH /api/boards/{id}/home, which is set-only and per-user:
// it writes the home board of the user behind the API key, which is also what
// GET /api/boards reports back in isHome/isMobileHome.
//
// Clearing has no board-side endpoint and goes through
// PATCH /api/users/preferences with an explicit null, added in Homarr 2.3.0.
// It is attempted only when the prior state says this board actually holds the
// flag — clearing unconditionally would unset whichever other board currently
// holds the per-user singleton.
func (r *boardResource) applyHomeFlags(ctx context.Context, id string, plan, state boardModel, diags *diag.Diagnostics) {
	clear := client.UserPreferencesPatch{}

	switch {
	case boolSelected(plan.IsHome) && !state.IsHome.ValueBool():
		if err := r.client.SetBoardAsHome(ctx, id); err != nil {
			diags.AddAttributeError(
				path.Root("is_home"),
				"Unable to select the Homarr home board",
				"Homarr requires view access to a board before it can become a home board. "+err.Error(),
			)
		}
	case boolCleared(plan.IsHome) && state.IsHome.ValueBool():
		clear["homeBoardId"] = nil
	}

	switch {
	case boolSelected(plan.IsMobileHome) && !state.IsMobileHome.ValueBool():
		if err := r.client.SetBoardAsMobileHome(ctx, id); err != nil {
			diags.AddAttributeError(
				path.Root("is_mobile_home"),
				"Unable to select the Homarr mobile home board",
				"Homarr requires view access to a board before it can become a home board. "+err.Error(),
			)
		}
	case boolCleared(plan.IsMobileHome) && state.IsMobileHome.ValueBool():
		clear["mobileHomeBoardId"] = nil
	}

	if len(clear) == 0 {
		return
	}
	if _, err := r.client.UpdateUserPreferences(ctx, "", clear); err != nil {
		detail := "Clearing a home board needs PATCH /api/users/preferences, which Homarr added in 2.3.0. " +
			"On an older release, move the flag to another board instead of removing it. " + err.Error()
		diags.AddError("Unable to clear the Homarr home board", detail)
	}
}

func (r *boardResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state boardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	board, err := r.client.GetBoard(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr board", err.Error())
		return
	}

	state.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *boardResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state boardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Homarr splits board mutations across single-purpose endpoints, so each
	// changed attribute is its own call.
	if !plan.Name.Equal(state.Name) {
		if err := r.client.RenameBoard(ctx, id, plan.Name.ValueString()); err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("name"),
					"Board name already taken",
					"Homarr requires board names to be unique: "+err.Error(),
				)
				return
			}
			resp.Diagnostics.AddError("Unable to rename Homarr board", err.Error())
			return
		}
	}

	if !plan.IsPublic.Equal(state.IsPublic) {
		if err := r.client.SetBoardVisibility(ctx, id, plan.IsPublic.ValueBool()); err != nil {
			resp.Diagnostics.AddError(
				"Unable to change Homarr board visibility",
				"Homarr rejects making a board private while it is the instance-wide home board, "+
					"set through homarr_server_board_settings. "+err.Error(),
			)
			return
		}
	}

	r.applyHomeFlags(ctx, id, plan, state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	board, err := r.client.GetBoard(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr board back after update", err.Error())
		return
	}

	plan.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *boardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state boardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteBoard(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete Homarr board", err.Error())
	}
}

// ImportState imports a board using the composite id `<board_id>,<column_count>`.
//
// The column count has to be supplied by hand because Homarr's REST API never
// reports it. Importing with a bare id would leave the attribute null in state,
// and since a change to it forces replacement, the very next plan would propose
// destroying the imported board — so a bare id is rejected outright.
func (r *boardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ",")
	if len(parts) != 2 {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Import a board as \"<board_id>,<column_count>\", for example "+
				"\"csgjeb9j3y2mflvznts1ip0w,10\".\n\n"+
				"The column count cannot be read from Homarr's REST API, so it has to be stated explicitly. "+
				"Use the value shown under the board's layout settings in the Homarr UI (the UI default is 10). "+
				"Got: "+strconv.Quote(req.ID),
		)
		return
	}

	boardID := strings.TrimSpace(parts[0])
	if boardID == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"The board id part of \"<board_id>,<column_count>\" must not be empty.",
		)
		return
	}

	columnCount, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || columnCount < 1 || columnCount > 24 {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"The column count part of \"<board_id>,<column_count>\" must be an integer between 1 and 24. "+
				"Got: "+strconv.Quote(parts[1]),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), boardID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("column_count"), columnCount)...)
}
