package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &userPreferencesDataSource{}
	_ datasource.DataSourceWithConfigure = &userPreferencesDataSource{}
)

// NewUserPreferencesDataSource returns the homarr_user_preferences data source.
func NewUserPreferencesDataSource() datasource.DataSource { return &userPreferencesDataSource{} }

type userPreferencesDataSource struct {
	client *client.Client
}

type userPreferencesDataSourceModel struct {
	ID     types.String `tfsdk:"id"`
	UserID types.String `tfsdk:"user_id"`
	userPreferenceValues
}

func (d *userPreferencesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_preferences"
}

func (d *userPreferencesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := userPreferenceDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{
		Computed:            true,
		MarkdownDescription: "Same value as `user_id`, present because Terraform requires an `id`.",
	}
	attrs["user_id"] = schema.StringAttribute{
		Optional: true,
		Computed: true,
		MarkdownDescription: "Identifier of the user whose preferences to read. Leave it out to read those of " +
			"the user behind the API key, which any key may do; the attribute then reports that user's id. " +
			"Naming another user requires an admin key.",
		Validators: []validator.String{
			stringvalidator.LengthAtLeast(1),
		},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the preferences of a Homarr user: the read-only counterpart of the " +
			"`homarr_user_preferences` resource, for any account, including those backed by LDAP or OIDC.\n\n" +
			"Where the resource spells a cleared reference as `\"\"`, this data source reports `null`: " +
			"`home_board_id`, `mobile_home_board_id` and `default_search_engine_id` are `null` when the user " +
			"has none.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer**, which added `GET /api/users/preferences`. On an older " +
			"release `user_id` must be set: the preferences are then read from `GET /api/users/{id}` instead, " +
			"and `header`, which only the newer endpoint reports, is `null`, with a warning.",
		Attributes: attrs,
	}
}

func (d *userPreferencesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *userPreferencesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userPreferencesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An empty userId targets the user behind the API key, and Homarr reports
	// that user's id in the response.
	userID := config.UserID.ValueString()
	prefs, err := d.client.GetUserPreferences(ctx, userID)
	if err == nil {
		var state userPreferencesDataSourceModel
		state.ID = types.StringValue(prefs.UserID)
		state.UserID = types.StringValue(prefs.UserID)
		state.userPreferenceValues.fromAPI(prefs.UserPreferenceFields)
		state.Header = headerDataSourceValue(prefs.HeaderPreferences)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	if !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to read Homarr user preferences", err.Error())
		return
	}
	if userID == "" {
		// The API key's own user always exists, so only the endpoint itself
		// can be missing.
		resp.Diagnostics.AddError(
			"Unable to read Homarr user preferences",
			"Reading the preferences of the API key's user requires Homarr 2.3.0 or newer: "+
				"GET /api/users/preferences answered 404, so this Homarr predates it. Set user_id to read them "+
				"from an older release. "+err.Error(),
		)
		return
	}

	// A 404 is ambiguous: Homarr answers it for a user that does not exist, for
	// another user when the key is not an admin, and — before 2.3.0 — for the
	// endpoint itself. GET /api/users/{id} tells the three apart, and in the
	// last case reports the preferences too, all but the header.
	user, userErr := d.client.GetUser(ctx, userID)
	if userErr != nil {
		switch {
		case client.IsNotFound(userErr):
			resp.Diagnostics.AddAttributeError(
				path.Root("user_id"),
				"User not found",
				"Homarr has no user with id "+userID+". "+userErr.Error(),
			)
		case client.IsForbidden(userErr):
			resp.Diagnostics.AddAttributeError(
				path.Root("user_id"),
				"API key cannot read this Homarr user's preferences",
				"The API key is not an admin, so it can read only its own user's preferences, not those of "+
					userID+". Use an admin key, or leave user_id out to read the key's own. "+userErr.Error(),
			)
		default:
			resp.Diagnostics.AddError("Unable to read Homarr user preferences", userErr.Error())
		}
		return
	}

	resp.Diagnostics.AddWarning(
		"Homarr user header layout is unavailable",
		"The user exists, but GET /api/users/preferences answered 404, which means this Homarr predates "+
			"2.3.0. The preferences were read from GET /api/users/{id} instead, which does not report the "+
			"header in a usable form, so header is null.",
	)
	var state userPreferencesDataSourceModel
	state.ID = types.StringValue(user.ID)
	state.UserID = types.StringValue(user.ID)
	state.userPreferenceValues.fromAPI(user.UserPreferenceFields)
	state.Header = types.ObjectNull(headerAttrTypes)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
