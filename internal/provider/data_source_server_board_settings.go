package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &serverBoardSettingsDataSource{}
	_ datasource.DataSourceWithConfigure = &serverBoardSettingsDataSource{}
)

// NewServerBoardSettingsDataSource returns the homarr_server_board_settings
// data source.
func NewServerBoardSettingsDataSource() datasource.DataSource {
	return &serverBoardSettingsDataSource{}
}

type serverBoardSettingsDataSource struct {
	client *client.Client
}

type serverBoardSettingsDataSourceModel struct {
	HomeBoardID           types.String `tfsdk:"home_board_id"`
	MobileHomeBoardID     types.String `tfsdk:"mobile_home_board_id"`
	EnableStatusByDefault types.Bool   `tfsdk:"enable_status_by_default"`
	ForceDisableStatus    types.Bool   `tfsdk:"force_disable_status"`
}

func (d *serverBoardSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_board_settings"
}

func (d *serverBoardSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the instance-wide board defaults under *Management > Settings > Board* " +
			"without taking ownership of them, as the `homarr_server_board_settings` resource would. Requires " +
			"an admin API key.\n\n" +
			"Homarr reports the home boards as stored and does not check them on the way out: after onboarding " +
			"both point at Homarr's private onboarding board, and a board deleted since keeps its id here.",
		Attributes: map[string]schema.Attribute{
			"home_board_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Board shown on desktop to anonymous visitors and to users with no personal " +
					"or group home board, or null when none is set.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Board shown on mobile to anonymous visitors and to users with no personal " +
					"or group home board, or null when none is set, in which case mobile falls back to " +
					"`home_board_id`.",
			},
			"enable_status_by_default": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether newly added board items show their online status by default.",
			},
			"force_disable_status": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether online-status pings are disabled across every board.",
			},
		},
	}
}

func (d *serverBoardSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *serverBoardSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	settings, err := d.client.GetServerBoardSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr server board settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &serverBoardSettingsDataSourceModel{
		HomeBoardID:           stringValueOrNull(settings.HomeBoardID),
		MobileHomeBoardID:     stringValueOrNull(settings.MobileHomeBoardID),
		EnableStatusByDefault: types.BoolValue(settings.EnableStatusByDefault),
		ForceDisableStatus:    types.BoolValue(settings.ForceDisableStatus),
	})...)
}
