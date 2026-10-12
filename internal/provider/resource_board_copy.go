package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ resource.Resource                = &boardCopyResource{}
	_ resource.ResourceWithConfigure   = &boardCopyResource{}
	_ resource.ResourceWithImportState = &boardCopyResource{}
	_ resource.ResourceWithModifyPlan  = &boardCopyResource{}
	_ resource.ResourceWithIdentity    = &boardCopyResource{}
)

// NewBoardCopyResource returns the homarr_board_copy resource.
func NewBoardCopyResource() resource.Resource { return &boardCopyResource{} }

type boardCopyResource struct {
	client *client.Client
}

// boardCopyModel is boardModel with source_board_id in place of column_count:
// a copy inherits its layouts from the source, and their column counts are as
// unreadable as any other board's.
type boardCopyModel struct {
	ID            types.String `tfsdk:"id"`
	SourceBoardID types.String `tfsdk:"source_board_id"`
	Name          types.String `tfsdk:"name"`
	IsPublic      types.Bool   `tfsdk:"is_public"`
	LogoImageURL  types.String `tfsdk:"logo_image_url"`
	CreatorID     types.String `tfsdk:"creator_id"`
	IsHome        types.Bool   `tfsdk:"is_home"`
	IsMobileHome  types.Bool   `tfsdk:"is_mobile_home"`
}

func (r *boardCopyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board_copy"
}

func (r *boardCopyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a Homarr board by copying an existing one, tiles included, and then " +
			"manages the copy like `homarr_board`.\n\n" +
			"Homarr's REST API cannot read tiles back, so `homarr_board` can only create empty boards. Copying " +
			"(`POST /api/boards/{id}/duplicate`, present since Homarr 2.0.0) is the way to reproduce a board " +
			"designed in the UI: design a template there once, then stamp out copies with Terraform. Everything " +
			"on the source is copied in one transaction — its appearance settings and visibility, every layout " +
			"with its column and gutter counts, the sections, and the tiles with their options, positions and " +
			"integration links. The copy belongs to the user behind the API key.\n\n" +
			"~> **The copy is a snapshot, not a sync.** Later changes to the source are not carried over, and " +
			"the copied content — tiles, layouts, column counts, appearance — is neither refreshed nor compared " +
			"afterwards. Only `name`, `is_public`, `is_home` and `is_mobile_home` are managed; use " +
			"`homarr_board_settings` to override the inherited appearance.\n\n" +
			"~> **Replacing a copy discards everything done to it since.** A new `source_board_id` destroys the " +
			"copy, with any tiles added or moved in the UI, and copies the source afresh under a new id, which " +
			"also changes everything that references it, such as `homarr_server_board_settings`. That includes " +
			"the source being replaced: if the template is a `homarr_board` whose `column_count` changes, every " +
			"copy of it is replaced too. Add `lifecycle { ignore_changes = [source_board_id] }` to keep copies " +
			"through that.\n\n" +
			"Homarr checks the whole source while copying, and refuses the copy when:\n\n" +
			"* the API key lacks the `board-create` permission, or view access to the source — the latter " +
			"answered with 404, like a missing board;\n" +
			"* a tile links an integration the key's user may not *use* (404 `Integration not found`);\n" +
			"* a tile is a widget that requires an integration but has none — as happens when its only " +
			"integration is deleted, which unlinks it silently (400 `<kind> requires an integration`);\n" +
			"* the source contains custom widgets and the key is not an admin's.\n\n" +
			"Unlike `homarr_board`, a copy is never made its creator's home board automatically. App tiles on " +
			"the copy point at the same `homarr_app` ids as the source's.\n\n" +
			"Renaming, changing `is_public` and destroying the copy need full access to it, which its creator " +
			"always has.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr-generated identifier of the copy.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_board_id": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Identifier of the board to copy. The API key needs view access to it.\n\n" +
					"Homarr keeps no link from a copy to its source, so this cannot be read back. Changing " +
					"it replaces the copy. After an import it is unknown to the provider, and the next apply " +
					"records the configured value in place, without replacing the board.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						sourceBoardChanged,
						"Changing the source board of a copy that recorded one forces a new copy.",
						"Changing the source board of a copy that recorded one forces a new copy.",
					),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Name of the copy, also used in its URL. Same rules as `homarr_board`'s " +
					"`name`: only ASCII letters, digits, `-` and `_`, unique compared case-insensitively. " +
					"Renaming happens in place. Homarr refuses a taken name when copying and renaming; the " +
					"provider warns about one while planning.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					stringvalidator.RegexMatches(
						boardNamePattern,
						"must contain only ASCII letters, digits, hyphens and underscores",
					),
				},
			},
			"is_public": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether the copy is visible without authentication. Leave it out to keep " +
					"whatever the copy inherited from the source and to ignore later changes made in the UI.\n\n" +
					"Homarr copies the source's visibility and offers no way to choose another one in the same " +
					"call, so a copy of a public board set to `false` is public for the moment between the " +
					"copy and the visibility change. Should that change fail, the copy stays public, with a " +
					"warning saying so, until the next apply replaces it.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"logo_image_url": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Board logo URL, as reported by Homarr — inherited from the source unless " +
					"`homarr_board_settings` sets another.",
			},
			"creator_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Identifier of the user that created the copy: the user behind the API key, " +
					"unless the copy was imported.",
			},
			"is_home": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether the copy is the desktop home board of the user behind the API key. " +
					"Behaves exactly like `is_home` on `homarr_board`, and shares its lock, so the two resources " +
					"can hand the flag over within one apply. Setting it to `false` requires Homarr 2.3.0 or " +
					"newer.",
			},
			"is_mobile_home": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Whether the copy is the mobile home board of the user behind the API key. " +
					"Behaves exactly like `is_mobile_home` on `homarr_board`.",
			},
		},
	}
}

// sourceBoardChanged makes a new source_board_id force a new copy, except when
// state holds none: after an import, recording the configured value is all
// there is to do.
func sourceBoardChanged(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = !req.StateValue.IsNull()
}

// IdentitySchema identifies a copy by its board id. Unlike homarr_board, whose
// import also needs the column count, a copy is imported by the id alone — its
// source is recorded from the configuration afterwards — so the id makes a
// complete identity. See resource_identity.go.
func (r *boardCopyResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema("id", "Homarr-generated identifier of the copy.")
}

func (r *boardCopyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies the readable board fields into the model. source_board_id is
// left alone: Homarr does not record where a board was copied from.
func (m *boardCopyModel) fromAPI(board *client.BoardSummary) {
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

// nullUnknowns is boardModel.nullUnknowns for a copy.
func (m *boardCopyModel) nullUnknowns() {
	if m.IsPublic.IsUnknown() {
		m.IsPublic = types.BoolNull()
	}
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

func (r *boardCopyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan boardCopyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Homarr checks the name while copying, but only against the boards that
	// exist by then; see boardNameMu.
	boardNameMu.Lock()
	id, err := r.client.DuplicateBoard(ctx, plan.SourceBoardID.ValueString(), plan.Name.ValueString())
	boardNameMu.Unlock()
	if err != nil {
		reportCopyError(plan.SourceBoardID.ValueString(), err, &resp.Diagnostics)
		return
	}

	// As in homarr_board.Create, failures past this point are held back until
	// the copy is in state, so that it is tainted rather than leaked.
	var followUp diag.Diagnostics
	privateRefused := false
	if !plan.IsPublic.IsUnknown() && !plan.IsPublic.IsNull() {
		// The copy inherited the source's visibility; setting it is idempotent,
		// so it is sent without checking what was inherited.
		changed := changeBoardVisibility(ctx, r.client, id, plan.IsPublic.ValueBool(), &followUp)
		privateRefused = !changed && !plan.IsPublic.ValueBool()
	}
	applyBoardHomeFlags(ctx, r.client, id, plan.IsHome, plan.IsMobileHome, &followUp)

	board, err := r.client.GetBoard(ctx, id)
	if err != nil {
		plan.ID = types.StringValue(id)
		plan.nullUnknowns()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
		resp.Diagnostics.Append(followUp...)
		if privateRefused {
			addCopyKeptVisibilityWarning(&resp.Diagnostics, "kept the source board's visibility, public if "+
				"the source is, and keeps it")
		}
		resp.Diagnostics.AddError("Unable to read Homarr board copy back after creation", err.Error())
		return
	}

	if privateRefused && board.IsPublic {
		addCopyKeptVisibilityWarning(&followUp, "kept the source board's visibility and is public. It stays public")
	}
	plan.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
	resp.Diagnostics.Append(followUp...)
}

// addCopyKeptVisibilityWarning points out, after the visibility change of a new
// copy failed, that the copy may be visible to anyone: it inherited the source's
// visibility, and the error alone does not say so. state completes the sentence
// "The copy ...".
func addCopyKeptVisibilityWarning(diags *diag.Diagnostics, state string) {
	diags.AddAttributeWarning(
		path.Root("is_public"),
		"The Homarr board copy may be public",
		"Making the new copy private failed, so the copy "+state+" until the next apply replaces it, since "+
			"the failed create taints it. Until then, anonymous visitors can see whatever the copy shows.",
	)
}

// reportCopyError turns a failed POST /api/boards/{id}/duplicate into a
// diagnostic. Homarr answers several unrelated refusals with the same status,
// so its message decides which explanation applies.
func reportCopyError(sourceID string, err error, diags *diag.Diagnostics) {
	apiErr := homarrError(err)
	status, message := 0, ""
	if apiErr != nil {
		status, message = apiErr.StatusCode, apiErr.Message
	}

	switch {
	case status == http.StatusConflict:
		diags.AddAttributeError(
			path.Root("name"),
			"Board name already taken",
			"Homarr requires board names to be unique, compared case-insensitively: "+err.Error(),
		)
	case status == http.StatusNotFound && strings.Contains(message, "Integration"):
		diags.AddError(
			"Unable to copy Homarr board",
			"A tile on the source board links an integration that the API key's user may not use. Copying "+
				"a board needs use access to every integration its tiles link. "+err.Error(),
		)
	case status == http.StatusNotFound:
		diags.AddAttributeError(
			path.Root("source_board_id"),
			"Source board not found",
			"Homarr has no board "+sourceID+" that this API key can view. It answers 404 both for a missing "+
				"board and for one the key lacks view access to. "+err.Error(),
		)
	case status == http.StatusBadRequest && strings.Contains(message, "integration"):
		diags.AddError(
			"Unable to copy Homarr board",
			"Homarr re-validates every tile on the source board while copying it, and one of them has an "+
				"integration problem — typically a widget that requires an integration but has none, because "+
				"its integration was deleted. Fix the tile on the source board in the Homarr UI. "+err.Error(),
		)
	case status == http.StatusForbidden:
		diags.AddError(
			"Unable to copy Homarr board",
			"Copying a board requires the board-create permission, and an admin when the source contains "+
				"custom widgets. "+err.Error(),
		)
	default:
		diags.AddError("Unable to copy Homarr board", err.Error())
	}
}

func (r *boardCopyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state boardCopyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before the lookup, so that a refresh finding the copy gone still reports
	// one; see setIdentity.
	setIdentity(ctx, resp.Identity, "id", state.ID.ValueString(), &resp.Diagnostics)

	board, err := r.client.GetBoard(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr board copy", err.Error())
		return
	}

	state.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update applies the same in-place changes as homarr_board. A changed
// source_board_id only reaches Update when state had none, after an import, and
// needs no call: the plan already carries the value to record.
func (r *boardCopyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state boardCopyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if !plan.Name.Equal(state.Name) {
		if !renameBoard(ctx, r.client, id, plan.Name.ValueString(), &resp.Diagnostics) {
			return
		}
	}

	// An omitted is_public plans as the prior state, so only a configured
	// change reaches Homarr.
	if !plan.IsPublic.IsUnknown() && !plan.IsPublic.IsNull() && !plan.IsPublic.Equal(state.IsPublic) {
		if !changeBoardVisibility(ctx, r.client, id, plan.IsPublic.ValueBool(), &resp.Diagnostics) {
			return
		}
	}

	applyBoardHomeFlags(ctx, r.client, id, plan.IsHome, plan.IsMobileHome, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	board, err := r.client.GetBoard(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr board copy back after update", err.Error())
		return
	}

	plan.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
	if resp.Private != nil {
		forgetReplacedBoard(ctx, resp.Private, &resp.Diagnostics)
	}
}

func (r *boardCopyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state boardCopyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteBoard(ctx, r.client, state.ID.ValueString(), &resp.Diagnostics)
}

// ModifyPlan warns, while planning, about a name another board already uses.
// Homarr refuses it when copying and renaming; this only reports it earlier.
// See checkPlannedBoardName.
func (r *boardCopyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	modifyBoardNamePlan(ctx, r.client, req, resp)
}

// ImportState imports a copy by its board id, or by name=<name>; from
// Terraform 1.12 also by the id in an import block's identity. Any board can be
// imported this way, copied or not; source_board_id is recorded from the
// configuration on the next apply.
func (r *boardCopyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if id, byIdentity := importedID(ctx, req, "id", &resp.Diagnostics); byIdentity {
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
		return
	}

	key := strings.TrimSpace(req.ID)
	if key == "" {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Import a board copy by its board id or as \"name=<name>\".",
		)
		return
	}
	boardID, ok := importedBoardID(ctx, r.client, key, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), boardID)...)
	setIdentity(ctx, resp.Identity, "id", boardID, &resp.Diagnostics)
}
