package provider

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"

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

// homarr_board deliberately has no resource identity: the column count its
// import needs can be neither left out of one nor read back into one. See
// resource_identity.go.
var (
	_ resource.Resource                = &boardResource{}
	_ resource.ResourceWithConfigure   = &boardResource{}
	_ resource.ResourceWithImportState = &boardResource{}
	_ resource.ResourceWithModifyPlan  = &boardResource{}
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
	BaseLayoutID types.String `tfsdk:"base_layout_id"`
}

func (r *boardResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board"
}

func (r *boardResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr board — a dashboard page.\n\n" +
			"A new board is created exactly as the Homarr UI creates one: with one empty section and two " +
			"layouts, a 3-column *Mobile* layout and a *Base* layout with `column_count` columns. To start " +
			"from a board designed in the UI instead, use `homarr_board_copy`.\n\n" +
			"The widgets and app tiles placed *on* a board are not manageable here. Homarr's REST API can " +
			"add a tile (`POST /api/boards/items`, see the `homarr_board_add_item` action), move or resize " +
			"one, and rewrite a whole board (`PUT /api/boards/{id}/content`), but nothing over REST reads " +
			"tiles, sections or layouts back, so they cannot be reconciled declaratively.\n\n" +
			"Renaming, changing `is_public` and destroying a board need *full* access to it: being its " +
			"creator, a `full` user or group grant, or the `board-full-all` permission, which admins have. " +
			"Homarr answers anyone else with 404, which the provider reports as an access error while the " +
			"board is still listed.\n\n" +
			"~> **Note** Homarr makes a new board its creator's desktop home board whenever that user has none " +
			"selected — not only the first board, and again after `is_home = false` cleared the slot — so " +
			"`is_home` can come back `true` on a board that never asked for it. Declaring `is_home = false` " +
			"clears that again right after creation. `is_home` and " +
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
				MarkdownDescription: "Board name, also used in the board's URL. Only ASCII letters, " +
					"digits, `-` and `_` are allowed; spaces and other punctuation are rejected by Homarr.\n\n" +
					"Names must be unique, compared case-insensitively. Homarr's REST create does not check " +
					"that itself — an exact duplicate fails with an internal error, and one differing only in " +
					"case is accepted although the board's URL then reaches only one of the two — so the " +
					"provider checks the name against the boards the API key can see. It refuses a name " +
					"differing only in case right before creating the board. While planning it only warns, " +
					"because a plan cannot see another board being renamed or destroyed earlier in the same " +
					"apply, nor tell a tainted board being replaced from a clash.",
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
				MarkdownDescription: "Number of columns in the board's base layout, between 1 and 24. The " +
					"mobile layout always starts with 3.\n\n" +
					"~> **Note** The column count lives on the board's layouts, and no REST call reads layouts " +
					"back. Writing them (`PUT /api/boards/{id}/layouts`, Homarr 2.3.0) needs the complete " +
					"layout list including the mobile layout's id, which no REST response contains. Changing " +
					"this value therefore forces a new board, and the provider cannot detect a column count " +
					"changed through the Homarr UI.",
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
					"Homarr refuses to make a board private while it is an *instance-wide* home board — " +
					"`home_board_id` or `mobile_home_board_id` in `homarr_server_board_settings`, not the " +
					"per-user `is_home`.",
			},
			"logo_image_url": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Board logo URL, as reported by Homarr. Set this through " +
					"`homarr_board_settings`, which owns the board's appearance.",
			},
			"creator_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier of the user that created the board. That is the user behind " +
					"the API key only for boards that key created; an imported board keeps its original creator, " +
					"and the creator always has full access to the board.",
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
					"goes through `PATCH /api/users/preferences`, which arrived in 2.3.0 and is the first " +
					"endpoint that can address the API key's own user without knowing its id; older releases " +
					"can clear a home board only through `PATCH /api/users/changeHome` with an explicit user " +
					"id, which no REST call reveals for the key's own user. Homarr also refuses to make a board " +
					"private while it is an *instance-wide* home board set through `homarr_server_board_settings`.",
			},
			"is_mobile_home": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether this board is the mobile home board of the user behind the " +
					"API key. Behaves exactly like `is_home`, against Homarr's separate mobile slot. " +
					"Unlike `is_home` it is never assigned automatically.",
			},
			"base_layout_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier of the board's base layout — the desktop one — as Homarr " +
					"returned it when the provider created the board. It is the only layout id any REST response " +
					"contains. Pass it as `layout_id` to the `homarr_board_add_item` action to put a new tile at " +
					"a given position; other tools can use it with " +
					"`PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}` to move or resize a tile.\n\n" +
					"No REST call reads layouts back, so this is known only for boards created by provider " +
					"0.3.0 or newer. It is null after an import and for boards created earlier.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *boardResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies the readable board fields into the model.
//
// column_count and base_layout_id are deliberately untouched: boardSummarySchema
// carries neither, so the configured column count and the layout id from the
// create response are the only sources Terraform has.
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

// nullUnknowns replaces the values only a read-back fills in with null, so that
// a board whose read-back failed can still be written to state.
func (m *boardModel) nullUnknowns() {
	if m.LogoImageURL.IsUnknown() {
		m.LogoImageURL = types.StringNull()
	}
	if m.CreatorID.IsUnknown() {
		m.CreatorID = types.StringNull()
	}
	if m.IsHome.IsUnknown() {
		m.IsHome = types.BoolNull()
	}
	if m.IsMobileHome.IsUnknown() {
		m.IsMobileHome = types.BoolNull()
	}
}

func (r *boardResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan boardModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, refused, err := r.createBoard(ctx, plan, &resp.Diagnostics)
	if refused {
		return
	}
	if err != nil {
		if detail, taken := boardNameTakenOnCreate(ctx, r.client, plan.Name.ValueString(), err); taken {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Board name already taken", detail)
			return
		}
		resp.Diagnostics.AddError("Unable to create Homarr board", err.Error())
		return
	}
	id := created.BoardID
	plan.BaseLayoutID = stringValueOrNullIfEmpty(created.LayoutID)

	// The home-board flags have no field on POST /api/boards; they are separate
	// calls made against the freshly created board. Their diagnostics are held
	// back so that state is written first: the board exists either way, and
	// returning an error without state would leak it. Reporting the failure
	// afterwards taints the resource instead, which is recoverable.
	var homeDiags diag.Diagnostics
	r.applyHomeFlags(ctx, id, plan, &homeDiags)

	board, err := r.client.GetBoard(ctx, id)
	if err != nil {
		// The board exists, so it goes into state, tainted by the error, rather
		// than leaking.
		plan.ID = types.StringValue(id)
		plan.nullUnknowns()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		resp.Diagnostics.Append(homeDiags...)
		resp.Diagnostics.AddError("Unable to read Homarr board back after creation", err.Error())
		return
	}

	plan.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	resp.Diagnostics.Append(homeDiags...)
}

// createBoard checks the name and creates the board. refused is true when the
// name check added an error, and nothing was created.
//
// The check and the create run under boardNameMu, so that no other board's
// create, copy or rename lands between them: two boards whose names differ only
// in case, created in the same apply, would otherwise both pass the check, and
// Homarr would accept both.
//
// Creating a board can also write the home board: Homarr makes the new board
// home when the creator has none. That write must not interleave with another
// board's select or clear, so the create also holds homeBoardMu, taken after
// boardNameMu as everywhere both are held.
func (r *boardResource) createBoard(ctx context.Context, plan boardModel, diags *diag.Diagnostics) (created *client.BoardCreated, refused bool, err error) {
	boardNameMu.Lock()
	defer boardNameMu.Unlock()
	if !refuseCaseVariantBoardName(ctx, r.client, plan.Name.ValueString(), diags) {
		return nil, true, nil
	}

	homeBoardMu.Lock()
	defer homeBoardMu.Unlock()
	created, err = r.client.CreateBoard(ctx, client.BoardCreateRequest{
		Name:        plan.Name.ValueString(),
		ColumnCount: plan.ColumnCount.ValueInt64(),
		IsPublic:    plan.IsPublic.ValueBool(),
	})
	return created, false, err
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

// homeBoardMu serialises home-board changes across every homarr_board and
// homarr_board_copy in a run.
//
// The home board is a per-user singleton spread over several resources, and
// Terraform applies independent resources in parallel. Moving the flag from
// board B to board A is one board selecting and another clearing; without this
// lock B could confirm it holds the flag, lose the race to A selecting it, and
// then clear the flag A had just taken. Board creation takes it too, because
// Homarr may make the new board home as a side effect, and so do the
// preference writes of homarr_user and homarr_user_preferences that set a home
// board, because the user they target may be the one behind the API key.
var homeBoardMu sync.Mutex

// applyHomeFlags selects or clears the board as desktop and/or mobile home
// board.
//
// Selecting uses PATCH /api/boards/{id}/home, which is set-only and per-user:
// it writes the home board of the user behind the API key, which is also what
// GET /api/boards reports back in isHome/isMobileHome. It is sent whenever the
// plan asks for the flag, even if prior state already has it: the call is
// idempotent, and state can be stale — after `-refresh=false`, or when the flag
// moved between refresh and apply — which would otherwise leave the board
// without the flag the plan promised.
//
// Clearing has no board-side endpoint and goes through
// PATCH /api/users/preferences with an explicit null, added in Homarr 2.3.0.
// That null empties the user's slot whichever board holds it, so the board is
// re-read first and the slot is cleared only if this board holds it right now.
// Prior state cannot answer that: on create it is empty even though Homarr may
// have made the new board home automatically, and on update another board may
// have taken the flag earlier in the same apply.
func (r *boardResource) applyHomeFlags(ctx context.Context, id string, plan boardModel, diags *diag.Diagnostics) {
	applyBoardHomeFlags(ctx, r.client, id, plan.IsHome, plan.IsMobileHome, diags)
}

// applyBoardHomeFlags is applyHomeFlags for any board resource, given the
// planned is_home and is_mobile_home.
func applyBoardHomeFlags(ctx context.Context, c *client.Client, id string, isHome, isMobileHome types.Bool, diags *diag.Diagnostics) {
	selectHome := boolSelected(isHome)
	selectMobile := boolSelected(isMobileHome)
	clearHome := boolCleared(isHome)
	clearMobile := boolCleared(isMobileHome)
	if !selectHome && !selectMobile && !clearHome && !clearMobile {
		return
	}

	homeBoardMu.Lock()
	defer homeBoardMu.Unlock()

	if selectHome {
		if err := c.SetBoardAsHome(ctx, id); err != nil {
			diags.AddAttributeError(
				path.Root("is_home"),
				"Unable to select the Homarr home board",
				"Homarr requires view access to a board before it can become a home board. "+err.Error(),
			)
		}
	}
	if selectMobile {
		if err := c.SetBoardAsMobileHome(ctx, id); err != nil {
			diags.AddAttributeError(
				path.Root("is_mobile_home"),
				"Unable to select the Homarr mobile home board",
				"Homarr requires view access to a board before it can become a home board. "+err.Error(),
			)
		}
	}
	if !clearHome && !clearMobile {
		return
	}

	board, err := c.GetBoard(ctx, id)
	if err != nil {
		diags.AddError("Unable to check the Homarr home board before clearing it", err.Error())
		return
	}

	clear := client.UserPreferencesPatch{}
	if clearHome && board.IsHome {
		clear["homeBoardId"] = nil
	}
	if clearMobile && board.IsMobileHome {
		clear["mobileHomeBoardId"] = nil
	}
	if len(clear) == 0 {
		return
	}
	if _, err := c.UpdateUserPreferences(ctx, "", clear); err != nil {
		detail := err.Error()
		// The caller always exists, so a 404 here can only be the endpoint
		// itself missing.
		if client.IsNotFound(err) {
			detail = "Clearing a home board needs PATCH /api/users/preferences, which Homarr added in 2.3.0. " +
				"On an older release, move the flag to another board instead of removing it. " + detail
		}
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
		if !renameBoard(ctx, r.client, id, plan.Name.ValueString(), &resp.Diagnostics) {
			return
		}
	}

	if !plan.IsPublic.Equal(state.IsPublic) {
		if !changeBoardVisibility(ctx, r.client, id, plan.IsPublic.ValueBool(), &resp.Diagnostics) {
			return
		}
	}

	r.applyHomeFlags(ctx, id, plan, &resp.Diagnostics)
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
	if resp.Private != nil {
		forgetReplacedBoard(ctx, resp.Private, &resp.Diagnostics)
	}
}

func (r *boardResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state boardModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteBoard(ctx, r.client, state.ID.ValueString(), &resp.Diagnostics)
}

// ModifyPlan warns, while planning, about a name another board already uses.
// See checkPlannedBoardName.
func (r *boardResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	modifyBoardNamePlan(ctx, r.client, req, resp)
}

// ImportState imports a board using the composite id `<board_id>,<column_count>`
// or `name=<name>,<column_count>`.
//
// The column count has to be supplied by hand because Homarr's REST API never
// reports it. Importing with a bare id would leave the attribute null in state,
// and since a change to it forces replacement, the very next plan would propose
// destroying the imported board — so a bare id is rejected outright.
//
// base_layout_id stays null: the base layout's id is returned only when a board
// is created.
func (r *boardResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, ",")
	if len(parts) != 2 {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Import a board as \"<board_id>,<column_count>\" or \"name=<name>,<column_count>\", for example "+
				"\"csgjeb9j3y2mflvznts1ip0w,10\" or \"name=infra,10\".\n\n"+
				"The column count cannot be read from Homarr's REST API, so it has to be stated explicitly. "+
				"Use the value shown under the board's layout settings in the Homarr UI (the UI default is 10). "+
				"Got: "+strconv.Quote(req.ID),
		)
		return
	}

	boardKey := strings.TrimSpace(parts[0])
	if boardKey == "" {
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

	boardID, ok := importedBoardID(ctx, r.client, boardKey, &resp.Diagnostics)
	if !ok {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), boardID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("column_count"), columnCount)...)
}
