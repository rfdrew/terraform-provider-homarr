package provider

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// homarr_server_board_settings deliberately has no resource identity: a
// singleton keyed by the constant "board" would share one with every other
// Homarr instance. See resource_identity.go.
var (
	_ resource.Resource                = &serverBoardSettingsResource{}
	_ resource.ResourceWithConfigure   = &serverBoardSettingsResource{}
	_ resource.ResourceWithImportState = &serverBoardSettingsResource{}
)

// NewServerBoardSettingsResource returns the homarr_server_board_settings
// resource.
func NewServerBoardSettingsResource() resource.Resource { return &serverBoardSettingsResource{} }

type serverBoardSettingsResource struct {
	client *client.Client
}

type serverBoardSettingsModel struct {
	ID                    types.String `tfsdk:"id"`
	HomeBoardID           types.String `tfsdk:"home_board_id"`
	MobileHomeBoardID     types.String `tfsdk:"mobile_home_board_id"`
	EnableStatusByDefault types.Bool   `tfsdk:"enable_status_by_default"`
	ForceDisableStatus    types.Bool   `tfsdk:"force_disable_status"`
}

func (r *serverBoardSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_board_settings"
}

func (r *serverBoardSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the instance-wide board defaults under *Management > Settings > Board*.\n\n" +
			"This is a singleton: Homarr has exactly one such settings object, so declare at most one of these " +
			"resources. Unlike `homarr_board_settings`, these values *are* readable, so drift is detected " +
			"normally.\n\n" +
			"Creating this resource adopts the existing settings rather than resetting them — only the " +
			"attributes you specify are sent. Because of that, `null` means \"not managed\" rather than " +
			"\"none\": to clear `home_board_id` or `mobile_home_board_id`, set it to `\"\"`. Destroying this " +
			"resource leaves the settings in place and simply removes them from state.\n\n" +
			"~> **Both home boards must be public boards, and Homarr may check the one you do not manage.** " +
			"Onboarding stores Homarr's onboarding board, which is private, in both slots, and deleting a board " +
			"leaves its id behind here. Homarr 2.4.1 and earlier check only the home boards a change sends, but " +
			"the release after 2.4.1 checks the stored result, so a private or deleted board left in either " +
			"slot makes every change fail — even one that sets only `force_disable_status`, and even saving the " +
			"board settings in Homarr's UI. Manage both home board attributes together, set to public boards or " +
			"to `\"\"`, or make the onboarding board public. When Homarr refuses a board, the error names the " +
			"attribute that holds it.\n\n" +
			"~> **Clear the home boards before destroying a board they point at.** Neither destroying this " +
			"resource nor deleting a board clears them, so destroying a configuration that holds both a " +
			"`homarr_board` and these settings pointing at it — as in the example below — leaves the deleted " +
			"board's id behind. Visitors who fall back to the instance home board then land on a board that no " +
			"longer exists, and the release after Homarr 2.4.1 refuses every later change to these settings " +
			"until the id is replaced, even adopting them again with an empty resource. Before such a destroy, " +
			"set `home_board_id` and `mobile_home_board_id` to `\"\"`, or to a board that stays, and apply.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Always `board`, the key of this settings object in Homarr.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown on desktop to anonymous visitors and to users with no personal " +
					"or group home board. Homarr requires it to reference a public board, and refuses to make " +
					"that board private while it is selected here. Set to `\"\"` to clear it; leaving it out keeps " +
					"whatever Homarr has. Once it is cleared, those visitors get \"No home board found\" instead — " +
					"on mobile too, unless `mobile_home_board_id` is set. Home boards chosen by users and groups, " +
					"including *everyone*, are separate settings and are not affected.\n\n" +
					"After onboarding this holds Homarr's onboarding board, which is private, so writing that id " +
					"back — after importing these settings, say — fails until the board is made public. Deleting " +
					"the board selected here does not clear it: the id is left dangling, and the release after " +
					"Homarr 2.4.1 refuses every change to these settings until it is replaced or cleared, whether " +
					"or not this attribute is managed.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown on mobile to anonymous visitors and to users with no personal " +
					"or group home board. Homarr requires it to reference a public board, and refuses to make " +
					"that board private while it is selected here. Set to `\"\"` to clear it; leaving it out keeps " +
					"whatever Homarr has. Once it is cleared, mobile falls back to `home_board_id`.\n\n" +
					"Like `home_board_id`, this holds Homarr's private onboarding board after onboarding, and keeps " +
					"the id of a board deleted since; the release after Homarr 2.4.1 refuses every change to " +
					"these settings while it does, whether or not this attribute is managed.",
			},
			"enable_status_by_default": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether newly added board items show their online status by default.",
			},
			"force_disable_status": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Disable online-status pings across every board, overriding per-board settings.",
			},
		},
	}
}

func (r *serverBoardSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies the settings Homarr reports into the model.
//
// A cleared home board reads back as null, but this resource asks for that
// with "", so a "" already in the model is kept rather than replaced by null.
func (m *serverBoardSettingsModel) fromAPI(settings *client.ServerBoardSettings) {
	m.ID = types.StringValue("board")
	m.HomeBoardID = stringValueOrNullKeepingEmpty(settings.HomeBoardID, m.HomeBoardID)
	m.MobileHomeBoardID = stringValueOrNullKeepingEmpty(settings.MobileHomeBoardID, m.MobileHomeBoardID)
	m.EnableStatusByDefault = types.BoolValue(settings.EnableStatusByDefault)
	m.ForceDisableStatus = types.BoolValue(settings.ForceDisableStatus)
}

// patch collects the attributes present in the plan, so unmanaged settings keep
// their current values.
//
// The attributes are Optional+Computed, so a null or unknown home board only
// means "not configured" and its key is left out. Clearing one is spelled "",
// which goes out as an explicit null: Homarr would reject "" itself as a board
// id that is not a public board.
func (m *serverBoardSettingsModel) patch() client.ServerBoardSettingsPatch {
	patch := client.ServerBoardSettingsPatch{}

	putBoard := func(key string, v types.String) {
		switch {
		case v.IsNull() || v.IsUnknown():
			// Not configured: leave the key out.
		case v.ValueString() == "":
			patch[key] = nil
		default:
			patch[key] = v.ValueString()
		}
	}
	putBool := func(key string, v types.Bool) {
		if !v.IsNull() && !v.IsUnknown() {
			patch[key] = v.ValueBool()
		}
	}

	putBoard("homeBoardId", m.HomeBoardID)
	putBoard("mobileHomeBoardId", m.MobileHomeBoardID)
	putBool("enableStatusByDefault", m.EnableStatusByDefault)
	putBool("forceDisableStatus", m.ForceDisableStatus)
	return patch
}

// apply sends the plan's patch and refreshes the model from Homarr's answer,
// which always carries the full settings.
func (r *serverBoardSettingsResource) apply(ctx context.Context, plan *serverBoardSettingsModel) error {
	settings, err := r.client.UpdateServerBoardSettings(ctx, plan.patch())
	if err != nil {
		return err
	}
	plan.fromAPI(settings)
	return nil
}

// refusedHomeBoardsPrefix starts the 400 Homarr answers when a home board it
// would store is not a public board — private, or deleted. The ids it refused
// follow, separated by ", ".
const refusedHomeBoardsPrefix = "Board settings home board IDs must reference public boards:"

// refusedHomeBoardIDs returns the board ids named by that 400, in Homarr's
// order and with repeats, and false for any other error. Homarr names a board
// once for every slot it checked that holds it, so a board in both slots
// appears twice.
func refusedHomeBoardIDs(err error) ([]string, bool) {
	var apiErr *client.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest ||
		!strings.HasPrefix(apiErr.Message, refusedHomeBoardsPrefix) {
		return nil, false
	}
	var ids []string
	for _, id := range strings.Split(strings.TrimPrefix(apiErr.Message, refusedHomeBoardsPrefix), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, len(ids) > 0
}

// addApplyError reports a failed PATCH, pinning a refused home board on the
// attribute that holds it.
//
// Homarr 2.4.1 and earlier check only the home board ids a patch sends, so a
// refused id is one this configuration asked for. The release after 2.4.1
// checks the merged result instead, so an id can also come from a slot the
// patch left alone: Homarr's private onboarding board, or a board deleted
// since. Those are found by reading the stored settings, and explained
// differently, because the attribute holding them may not even be managed.
//
// Homarr names a board once for every slot holding it, so each slot the patch
// sent with a refused board accounts for one mention. Only mentions left over
// point at stored slots: Homarr 2.4.1 never leaves any, so it never costs the
// extra read, and the release after it does when the same board is both sent
// for one slot and stored in the other — the onboarding board, typically.
func (r *serverBoardSettingsResource) addApplyError(ctx context.Context, plan *serverBoardSettingsModel, err error, diags *diag.Diagnostics) {
	ids, ok := refusedHomeBoardIDs(err)
	if !ok {
		diags.AddError("Unable to update Homarr server board settings", err.Error())
		return
	}

	patch := plan.patch()
	slots := []struct {
		key       string
		attribute string
		stored    func(*client.ServerBoardSettings) *string
	}{
		{"homeBoardId", "home_board_id", func(s *client.ServerBoardSettings) *string { return s.HomeBoardID }},
		{"mobileHomeBoardId", "mobile_home_board_id", func(s *client.ServerBoardSettings) *string { return s.MobileHomeBoardID }},
	}

	// unplaced counts the mentions of each refused board that no sent slot
	// accounts for; order keeps Homarr's order for reporting what is left.
	unplaced := map[string]int{}
	var order []string
	for _, id := range ids {
		if _, seen := unplaced[id]; !seen {
			order = append(order, id)
		}
		unplaced[id]++
	}

	for _, slot := range slots {
		id, sent := patch[slot.key].(string)
		if !sent || !slices.Contains(ids, id) {
			continue
		}
		if unplaced[id] > 0 {
			unplaced[id]--
		}
		diags.AddAttributeError(
			path.Root(slot.attribute),
			"Home board is not a public board",
			"Homarr refused board "+id+" for "+slot.attribute+": an instance home board must be a "+
				"public board, and this one is private or does not exist. Make it public "+
				"(is_public = true on its homarr_board), or choose another board.",
		)
	}

	// The stored settings are read only for a mention left over, and at most
	// once; if that read fails too, the board is reported without an
	// attribute.
	placed := map[string]bool{}
	if slices.ContainsFunc(order, func(id string) bool { return unplaced[id] > 0 }) {
		if stored, _ := r.client.GetServerBoardSettings(ctx); stored != nil {
			for _, slot := range slots {
				if _, inPatch := patch[slot.key]; inPatch {
					continue
				}
				current := slot.stored(stored)
				if current == nil || unplaced[*current] == 0 {
					continue
				}
				id := *current
				placed[id] = true
				diags.AddAttributeError(
					path.Root(slot.attribute),
					"Stored home board is not a public board",
					"Homarr refused to change the server board settings because "+slot.attribute+" still "+
						"holds board "+id+", which is private or no longer exists. This configuration does not "+
						"set "+slot.attribute+", but newer Homarr releases check the stored home boards on "+
						"every change, not only those a change sends; until it is fixed, saving the board "+
						"settings in Homarr's UI fails the same way. After onboarding both slots hold Homarr's "+
						"private onboarding board, and deleting a board leaves its id behind. Set "+
						slot.attribute+" in this resource to a public board or to \"\", or make board "+id+
						" public (is_public = true on its homarr_board).",
				)
			}
		}
	}

	for _, id := range order {
		if unplaced[id] == 0 || placed[id] {
			continue
		}
		diags.AddError(
			"Home board is not a public board",
			"Homarr refused board "+id+": both instance home boards must be public boards, and this one "+
				"is private or no longer exists. Set home_board_id and mobile_home_board_id to public "+
				"boards or to \"\", or make board "+id+" public. "+err.Error(),
		)
	}
}

func (r *serverBoardSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverBoardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		r.addApplyError(ctx, &plan, err, &resp.Diagnostics)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serverBoardSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverBoardSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, err := r.client.GetServerBoardSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr server board settings", err.Error())
		return
	}

	state.fromAPI(settings)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *serverBoardSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan serverBoardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		r.addApplyError(ctx, &plan, err, &resp.Diagnostics)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the singleton from state without changing the server. Homarr
// has no notion of deleting its settings object, and the provider does not know
// what the values were before it took over.
//
// The home boards stay as well, which matters when the same destroy deletes a
// board they point at: neither this resource nor Homarr clears the id, so the
// warning names the boards still selected.
func (r *serverBoardSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	detail := "homarr_server_board_settings has been removed from Terraform state, but the settings remain in " +
		"effect. Homarr's settings object cannot be deleted, only overwritten."

	var state serverBoardSettingsModel
	if diags := req.State.Get(ctx, &state); !diags.HasError() {
		var kept []string
		for _, id := range []types.String{state.HomeBoardID, state.MobileHomeBoardID} {
			if id.ValueString() != "" && !slices.Contains(kept, id.ValueString()) {
				kept = append(kept, id.ValueString())
			}
		}
		if len(kept) > 0 {
			detail += " The home boards stay selected too (board " + strings.Join(kept, ", board ") + "). If " +
				"one of them is being destroyed as well, its id is left behind: visitors who fall back to the " +
				"instance home board land on a board that no longer exists, and the release after Homarr 2.4.1 " +
				"refuses every change to these settings, including from its UI, until that home board is set " +
				"to a public board or to \"\"."
		}
	}

	resp.Diagnostics.AddWarning("Homarr server board settings were left unchanged", detail)
}

// ImportState adopts the existing settings. The import id is ignored because
// there is only one settings object.
func (r *serverBoardSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "board")...)
}
