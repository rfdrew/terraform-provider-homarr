package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &usersDataSource{}
	_ datasource.DataSourceWithConfigure = &usersDataSource{}
)

// NewUsersDataSource returns the homarr_users data source.
func NewUsersDataSource() datasource.DataSource { return &usersDataSource{} }

type usersDataSource struct {
	client *client.Client
}

type userSummaryModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Email types.String `tfsdk:"email"`
	Image types.String `tfsdk:"image"`
}

type usersDataSourceModel struct {
	Users []userSummaryModel `tfsdk:"users"`
}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Homarr user. Requires an admin API key.\n\n" +
			"Useful for resolving the identifier of a pre-existing account — for instance to hand it to " +
			"`homarr_server_board_settings` — without importing it as a managed resource.",
		Attributes: map[string]schema.Attribute{
			"users": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "All users, in the order Homarr returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":    schema.StringAttribute{Computed: true, MarkdownDescription: "Identifier of the user."},
						"name":  schema.StringAttribute{Computed: true, MarkdownDescription: "Login name of the user, lowercased by Homarr."},
						"email": schema.StringAttribute{Computed: true, MarkdownDescription: "Email address of the user, if any."},
						"image": schema.StringAttribute{Computed: true, MarkdownDescription: "Profile image URL of the user, if any."},
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *usersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	users, err := d.client.ListUsers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Homarr users", err.Error())
		return
	}

	state := usersDataSourceModel{Users: make([]userSummaryModel, 0, len(users))}
	for i := range users {
		state.Users = append(state.Users, userSummaryModel{
			ID:    types.StringValue(users[i].ID),
			Name:  types.StringValue(users[i].Name),
			Email: stringValueOrNull(users[i].Email),
			Image: stringValueOrNull(users[i].Image),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

var (
	_ datasource.DataSource              = &infoDataSource{}
	_ datasource.DataSourceWithConfigure = &infoDataSource{}
)

// NewInfoDataSource returns the homarr_info data source.
func NewInfoDataSource() datasource.DataSource { return &infoDataSource{} }

type infoDataSource struct {
	client *client.Client
}

type infoDataSourceModel struct {
	ID      types.String `tfsdk:"id"`
	Version types.String `tfsdk:"version"`
}

func (d *infoDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_info"
}

func (d *infoDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reports the version of the Homarr instance, useful for guarding configuration " +
			"against older releases.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Always `info`; present because Terraform requires an `id`.",
			},
			"version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr version, e.g. `1.73.0`.",
			},
		},
	}
}

func (d *infoDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *infoDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	info, err := d.client.GetInfo(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr info", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &infoDataSourceModel{
		ID:      types.StringValue("info"),
		Version: types.StringValue(info.Version),
	})...)
}
