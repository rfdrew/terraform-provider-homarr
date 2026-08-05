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
			"attributes you specify are sent. Destroying it leaves the settings in place and simply removes " +
			"them from state.",
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
				MarkdownDescription: "Board shown to users on desktop when they have no personal home board. " +
					"Homarr requires it to reference a public board.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Board shown to users on mobile when they have no personal home board.",
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

func (m *serverBoardSettingsModel) fromAPI(settings *client.ServerBoardSettings) {
	m.ID = types.StringValue("board")
	m.HomeBoardID = stringValueOrNull(settings.HomeBoardID)
	m.MobileHomeBoardID = stringValueOrNull(settings.MobileHomeBoardID)
	m.EnableStatusByDefault = types.BoolValue(settings.EnableStatusByDefault)
	m.ForceDisableStatus = types.BoolValue(settings.ForceDisableStatus)
}

// apply sends only the attributes present in the plan, so unmanaged settings
// keep their current values.
func (r *serverBoardSettingsResource) apply(ctx context.Context, plan *serverBoardSettingsModel) error {
	settings, err := r.client.UpdateServerBoardSettings(ctx, client.ServerBoardSettingsPatch{
		HomeBoardID:           stringPointerOrNil(plan.HomeBoardID),
		MobileHomeBoardID:     stringPointerOrNil(plan.MobileHomeBoardID),
		EnableStatusByDefault: boolPointerOrNil(plan.EnableStatusByDefault),
		ForceDisableStatus:    boolPointerOrNil(plan.ForceDisableStatus),
	})
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
