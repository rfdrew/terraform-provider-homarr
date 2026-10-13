package provider

import (
	"cmp"
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Email    types.String `tfsdk:"email"`
	Image    types.String `tfsdk:"image"`
	Provider types.String `tfsdk:"provider_name"`
}

type usersDataSourceModel struct {
	Provider types.String       `tfsdk:"provider_name"`
	Users    []userSummaryModel `tfsdk:"users"`
}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Homarr user, or those of one auth provider.\n\n" +
			"Any API key may read it. Homarr reserves `GET /api/users` for admins, so for another key the " +
			"provider falls back to `GET /api/users/selectable`, the list behind Homarr's user pickers, which " +
			"reports the same attributes. The auth provider of each user comes from that endpoint too, asked " +
			"once per provider.\n\n" +
			"To find a single user, the `homarr_user` data source is simpler: it looks one up by id or by name.",
		Attributes: map[string]schema.Attribute{
			"provider_name": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Only list the users who sign in through this auth provider: `credentials`, " +
					"`oidc` or `ldap`. Leave it out to list everyone.",
				Validators: []validator.String{
					stringvalidator.OneOf(client.AuthProviders...),
				},
			},
			"users": schema.ListNestedAttribute{
				Computed: true,
				MarkdownDescription: "All users, sorted by name and then by id, with users that have no name " +
					"last. Homarr itself returns them in no particular order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Identifier of the user."},
						"name": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Login name of the user, or `null` for an account Homarr stores " +
								"without one. Homarr lowercases the names of credentials accounts, while LDAP and " +
								"OIDC accounts keep the name their provider supplies. Names are unique only per " +
								"auth provider: pair one with `provider_name` to tell such users apart.",
						},
						"provider_name": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Auth provider the user signs in through: `credentials`, `oidc` or " +
								"`ldap`. `null` for a provider this version of the provider does not know.",
						},
						"email": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Email address of the user, or `null` if it has none. An email " +
								"stored as `\"\"`, as the UI saves a blank one, reads as `null` too, as on the " +
								"`homarr_user` data source.",
						},
						"image": schema.StringAttribute{
							Computed: true,
							MarkdownDescription: "Profile image of the user, if any: a `data:` URL of an uploaded " +
								"picture, or the picture URL an OIDC provider supplied.",
						},
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *usersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config usersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	users, providers, err := d.listUsers(ctx, config.Provider.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Homarr users", err.Error())
		return
	}

	// Neither list has an ORDER BY, so the database decides the order and may
	// change it between reads.
	slices.SortFunc(users, compareUsers)

	state := usersDataSourceModel{Provider: config.Provider, Users: make([]userSummaryModel, 0, len(users))}
	for i := range users {
		state.Users = append(state.Users, userSummaryModel{
			ID:       types.StringValue(users[i].ID),
			Name:     stringValueOrNull(users[i].Name),
			Email:    emailValueOrNull(users[i].Email),
			Image:    stringValueOrNull(users[i].Image),
			Provider: stringValueOrNullIfEmpty(providers[users[i].ID]),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// listUsers returns the users to report, and the auth provider of each by id.
//
// With a provider, one request answers both. Without one, the list comes from
// GET /api/users, or its fallback, rather than from the per-provider lists
// alone, so that a user of a provider unknown here is still listed.
func (d *usersDataSource) listUsers(ctx context.Context, provider string) ([]client.UserSummary, map[string]string, error) {
	providers := map[string]string{}
	if provider != "" {
		users, err := d.client.ListSelectableUsers(ctx, provider)
		if err != nil {
			return nil, nil, err
		}
		for _, u := range users {
			providers[u.ID] = provider
		}
		return users, providers, nil
	}

	users, err := d.client.ListUsersWithAnyKey(ctx)
	if err != nil {
		return nil, nil, err
	}
	byProvider, err := d.client.ListUsersByProvider(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, u := range byProvider {
		providers[u.ID] = u.Provider
	}
	return users, providers, nil
}

// compareUsers orders users by name and then by id, putting users without a
// name last.
func compareUsers(a, b client.UserSummary) int {
	switch {
	case a.Name == nil && b.Name != nil:
		return 1
	case a.Name != nil && b.Name == nil:
		return -1
	case a.Name != nil && b.Name != nil && *a.Name != *b.Name:
		return cmp.Compare(*a.Name, *b.Name)
	}
	return cmp.Compare(a.ID, b.ID)
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
				MarkdownDescription: "Homarr version, e.g. `2.5.2`.",
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
