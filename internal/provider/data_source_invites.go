package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &invitesDataSource{}
	_ datasource.DataSourceWithConfigure = &invitesDataSource{}
)

// NewInvitesDataSource returns the homarr_invites data source.
func NewInvitesDataSource() datasource.DataSource { return &invitesDataSource{} }

type invitesDataSource struct {
	client *client.Client
}

type inviteSummaryModel struct {
	ID             types.String `tfsdk:"id"`
	ExpirationDate types.String `tfsdk:"expiration_date"`
	Expired        types.Bool   `tfsdk:"expired"`
	CreatorID      types.String `tfsdk:"creator_id"`
	CreatorName    types.String `tfsdk:"creator_name"`
}

type invitesDataSourceModel struct {
	Invites []inviteSummaryModel `tfsdk:"invites"`
}

func (d *invitesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invites"
}

func (d *invitesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the registration invites Homarr holds — every invite that has not been used " +
			"or deleted, including expired ones, which Homarr never removes on its own. Requires an admin API " +
			"key and the `credentials` auth provider.\n\n" +
			"Useful in a `check` block, for example to flag expired invites left behind, or to find the id of " +
			"an invite created in the UI so it can be imported as a `homarr_invite`. Tokens are never listed: " +
			"Homarr returns an invite's token only once, when it is created.",
		Attributes: map[string]schema.Attribute{
			"invites": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "All invites, ordered by expiration date, soonest first.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Identifier of the invite.",
						},
						"expiration_date": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Moment the invite stops working, as an RFC 3339 timestamp in UTC " +
								"such as `2027-01-01T00:00:00Z`.",
						},
						"expired": schema.BoolAttribute{
							Computed: true,
							MarkdownDescription: "Whether `expiration_date` had passed when the data source was " +
								"read, by the clock of the machine running Terraform. Homarr refuses an expired " +
								"invite but keeps it.",
						},
						"creator_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Identifier of the user that created the invite.",
						},
						"creator_name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Name of the user that created the invite.",
						},
					},
				},
			},
		},
	}
}

func (d *invitesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// inviteSummaryFromAPI maps one listed invite. The expiration is reported the
// way homarr_invite stores an imported one, RFC 3339 in UTC, so the two can be
// compared directly.
func inviteSummaryFromAPI(invite *client.Invite, now time.Time) inviteSummaryModel {
	summary := inviteSummaryModel{
		ID:             types.StringValue(invite.ID),
		ExpirationDate: types.StringValue(invite.ExpirationDate),
		Expired:        types.BoolNull(),
		CreatorID:      types.StringValue(invite.Creator.ID),
		CreatorName:    stringValueOrNull(invite.Creator.Name),
	}
	if parsed, err := time.Parse(time.RFC3339, invite.ExpirationDate); err == nil {
		summary.ExpirationDate = types.StringValue(parsed.UTC().Format(time.RFC3339))
		// Homarr refuses an invite once its expiration is earlier than now.
		summary.Expired = types.BoolValue(parsed.Before(now))
	}
	return summary
}

func (d *invitesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	invites, err := d.client.ListInvites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Homarr invites", err.Error())
		return
	}

	now := time.Now()
	state := invitesDataSourceModel{Invites: make([]inviteSummaryModel, 0, len(invites))}
	for i := range invites {
		state.Invites = append(state.Invites, inviteSummaryFromAPI(&invites[i], now))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
