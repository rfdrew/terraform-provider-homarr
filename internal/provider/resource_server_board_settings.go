package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

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
			"resource leaves the settings in place and simply removes them from state.",
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
					"including *everyone*, are separate settings and are not affected.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown on mobile to anonymous visitors and to users with no personal " +
					"or group home board. Homarr requires it to reference a public board, and refuses to make " +
					"that board private while it is selected here. Set to `\"\"` to clear it; leaving it out keeps " +
					"whatever Homarr has. Once it is cleared, mobile falls back to `home_board_id`.",
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

func (r *serverBoardSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverBoardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Unable to update Homarr server board settings", err.Error())
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
		resp.Diagnostics.AddError("Unable to update Homarr server board settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the singleton from state without changing the server. Homarr
// has no notion of deleting its settings object, and the provider does not know
// what the values were before it took over.
func (r *serverBoardSettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Homarr server board settings were left unchanged",
		"homarr_server_board_settings has been removed from Terraform state, but the settings remain in "+
			"effect. Homarr's settings object cannot be deleted, only overwritten.",
	)
}

// ImportState adopts the existing settings. The import id is ignored because
// there is only one settings object.
func (r *serverBoardSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "board")...)
}
