package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// This file holds the homarr_user and homarr_current_user data sources, and
// the read-only preference attributes they share with the
// homarr_user_preferences data source.

// userDataSourceModel is a user as the homarr_user and homarr_current_user data
// sources report it: the account, minus what Homarr never reports, and its
// preferences.
type userDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Provider types.String `tfsdk:"provider_name"`
	Email    types.String `tfsdk:"email"`
	Image    types.String `tfsdk:"image"`
	userPreferenceValues
}

// fromAPI copies the account and its scalar preferences into the model. The
// header is read separately, because only /api/users/preferences reports it
// parsed.
func (m *userDataSourceModel) fromAPI(user *client.UserDetail) {
	m.ID = types.StringValue(user.ID)
	m.Username = stringValueOrNull(user.Name)
	m.Provider = stringValueOrNullIfEmpty(user.Provider)

	// Like homarr_user, report an email stored as "" as no email at all.
	m.Email = emailValueOrNull(user.Email)
	m.Image = stringValueOrNull(user.Image)

	m.userPreferenceValues.fromAPI(user.UserPreferenceFields)
}

// userDataSourceAttributes returns the attributes of a reported user. With
// lookupKeys, id, username and provider_name become the optional inputs of a
// lookup, as homarr_user takes them; otherwise everything is computed, as for
// homarr_current_user.
func userDataSourceAttributes(lookupKeys bool) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the user.",
		},
		"username": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "Login name of the user, as Homarr stores it. `null` for an account Homarr " +
				"stores without one.",
		},
		"provider_name": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Auth provider the user signs in through: `credentials`, `oidc` or `ldap`.",
		},
		"email": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Email address of the user, or `null` when it has none.",
		},
		"image": schema.StringAttribute{
			Computed: true,
			MarkdownDescription: "Profile image of the user, or `null` when it has none: a `data:` URL of an " +
				"uploaded picture, which can be up to 350,000 characters long, or the picture URL an OIDC " +
				"provider supplied.",
		},
	}
	if lookupKeys {
		attrs["id"] = schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Identifier of the user to look up. Exactly one of `id` and `username` must be set.",
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		}
		attrs["username"] = schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "Login name of the user to look up. Exactly one of `id` and `username` must be " +
				"set.\n\n" +
				"Surrounding whitespace and case are ignored: Homarr lowercases the names of credentials " +
				"accounts, while LDAP and OIDC accounts keep the name their provider supplies. The attribute " +
				"then reports the name as Homarr stores it. Usernames are unique only per auth provider, so when " +
				"an LDAP or OIDC account shares the name of another account the lookup fails; set " +
				"`provider_name` to pick one.",
			Validators: []validator.String{
				stringvalidator.LengthAtLeast(1),
			},
		}
		attrs["provider_name"] = schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "Auth provider the user signs in through: `credentials`, `oidc` or `ldap`. " +
				"Set it along with `username` to only consider users of that provider; it cannot be combined " +
				"with `id`.",
			Validators: []validator.String{
				stringvalidator.OneOf(client.AuthProviders...),
				stringvalidator.ConflictsWith(path.MatchRoot("id")),
			},
		}
	}
	maps.Copy(attrs, userPreferenceDataSourceAttributes())
	return attrs
}

// userPreferenceDataSourceAttributes are the preferences as the data sources
// report them: the attributes of the resources, all computed. A reference that
// is not set is null.
func userPreferenceDataSourceAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"home_board_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Board shown to the user on desktop, or `null` for none.",
		},
		"mobile_home_board_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Board shown to the user on mobile, or `null` for none.",
		},
		"default_search_engine_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the search engine used by the search bar, or `null` for none.",
		},
		"color_scheme": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Interface colour scheme: `auto`, `light` or `dark`.",
		},
		"byte_unit_system": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "How byte sizes are displayed: `binary` (KiB, MiB) or `decimal` (kB, MB).",
		},
		"first_day_of_week": schema.Int64Attribute{
			Computed: true,
			MarkdownDescription: "First day of the week in calendar widgets, `0` for Sunday through `6` for " +
				"Saturday.",
		},
		"ping_icons_enabled": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether online-status ping indicators are shown as icons rather than dots.",
		},
		"enable_right_click_on_widgets": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether widgets handle right-clicks instead of the browser context menu.",
		},
		"open_search_in_new_tab": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether search results open in a new browser tab.",
		},
		"ddg_bangs": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether DuckDuckGo-style `!bang` shortcuts work in the search bar.",
		},
		"header": headerDataSourceAttribute(),
	}
}

// headerDataSourceAttribute is the read-only `header`. Its type is the one the
// resources take, so a layout read here can be assigned to them as a whole.
func headerDataSourceAttribute() schema.SingleNestedAttribute {
	zone := func(name string) schema.ListNestedAttribute {
		return schema.ListNestedAttribute{
			Computed:            true,
			MarkdownDescription: "Items in the " + name + " zone, in display order; `[]` when it is empty.",
			NestedObject: schema.NestedAttributeObject{
				Attributes: map[string]schema.Attribute{
					"builtin": schema.StringAttribute{
						Computed: true,
						MarkdownDescription: "The builtin control, such as `logo`, `search` or `user`, or `null` " +
							"for a board shortcut.",
					},
					"board_id": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "Identifier of the board linked to, or `null` for a builtin control.",
					},
				},
			},
		}
	}
	return schema.SingleNestedAttribute{
		Computed: true,
		MarkdownDescription: "Layout of the header bar shown above boards. It has the shape the `header` " +
			"attribute of `homarr_user` and `homarr_user_preferences` takes, so a layout read here can be " +
			"copied onto another user.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer.** On an older release it is `null`, and a warning says so.",
		Attributes: map[string]schema.Attribute{
			"visible": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the header is shown.",
			},
			"search_display": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the `search` item is shown: `input` or `icon`.",
			},
			"logo_display": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the `logo` item is shown: `logo` or `logoAndText`.",
			},
			"left":   zone("left"),
			"center": zone("center"),
			"right":  zone("right"),
		},
	}
}

// headerDataSourceValue converts the header Homarr reports for a data source.
// The resources keep the spelling of their configuration for an empty zone; a
// data source has none to keep, so an empty zone is [] — something a
// configuration can iterate over, unlike null — which the resources accept as
// an empty zone too.
func headerDataSourceValue(h *client.HeaderPreferences) types.Object {
	empty := types.ListValueMust(headerItemType, []attr.Value{})
	return headerValueFromAPI(h, types.ObjectValueMust(headerAttrTypes, map[string]attr.Value{
		"visible":        types.BoolNull(),
		"search_display": types.StringNull(),
		"logo_display":   types.StringNull(),
		"left":           empty,
		"center":         empty,
		"right":          empty,
	}))
}

// readDataSourceHeader reads a user's header layout for a data source, from
// GET /api/users/preferences.
//
// The user has just been read, so a 404 means a Homarr older than 2.3.0, which
// lacks the endpoint. The header is then null, with a warning, and the rest of
// the data source still works.
func readDataSourceHeader(ctx context.Context, c *client.Client, userID string, diags *diag.Diagnostics) types.Object {
	prefs, err := c.GetUserPreferences(ctx, userID)
	if err == nil {
		return headerDataSourceValue(prefs.HeaderPreferences)
	}
	if !client.IsNotFound(err) {
		diags.AddError("Unable to read the Homarr user's header layout", err.Error())
		return types.ObjectNull(headerAttrTypes)
	}
	diags.AddWarning(
		"Homarr user header layout is unavailable",
		"GET /api/users/preferences answered 404 although the user exists, which means this Homarr predates "+
			"2.3.0. header requires Homarr 2.3.0 or newer and is reported as null.",
	)
	return types.ObjectNull(headerAttrTypes)
}

// addUserReadError explains a failed GET /api/users/{id}: Homarr answers 404
// for a user that does not exist, and 403 when a key that is not an admin's
// asks for anyone but its own user.
func addUserReadError(diags *diag.Diagnostics, id string, err error) {
	switch {
	case client.IsNotFound(err):
		diags.AddError("Homarr user not found", "Homarr has no user with id "+id+". "+err.Error())
	case client.IsForbidden(err):
		diags.AddError(
			"API key cannot read this Homarr user",
			"The API key is not an admin, so Homarr lets it read only its own user, not "+id+". Use an admin "+
				"key, or homarr_current_user for the key's own account. "+err.Error(),
		)
	default:
		diags.AddError("Unable to read Homarr user", err.Error())
	}
}

var (
	_ datasource.DataSource                     = &userDataSource{}
	_ datasource.DataSourceWithConfigure        = &userDataSource{}
	_ datasource.DataSourceWithConfigValidators = &userDataSource{}
)

// NewUserDataSource returns the homarr_user data source.
func NewUserDataSource() datasource.DataSource { return &userDataSource{} }

type userDataSource struct {
	client *client.Client
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single Homarr user by `id` or by `username`. Exactly one of the two " +
			"must be set.\n\n" +
			"Reports the account and its preferences: the attributes of the `homarr_user` resource except the " +
			"password and the groups, which Homarr never reports, plus the profile image. Unlike the resource it " +
			"reads any account, including those backed by LDAP or OIDC, so it suits accounts that Terraform " +
			"should reference without managing them.\n\n" +
			"Any API key may use it. A lookup by `username` lists users through `GET /api/users/selectable`, " +
			"which is open to every key. Homarr shows the details of a user other than the key's own only to " +
			"an admin, though: for any other key the data source reports just what that list holds — `id`, " +
			"`username`, `provider_name`, `email` and `image` — leaves the preferences and `header` null, and " +
			"says so in a warning. For the key's own account, `homarr_current_user` needs no id or name.\n\n" +
			"~> **`header` requires Homarr 2.3.0 or newer.** On an older release it is `null`, and a warning " +
			"says so; everything else is read from `GET /api/users/{id}` and works on any 2.x release.",
		Attributes: userDataSourceAttributes(true),
	}
}

// ConfigValidators requires exactly one lookup key.
func (d *userDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("username")),
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	if config.ID.IsNull() {
		id = userIDByName(ctx, d.client, config.Username.ValueString(), config.Provider.ValueString(), &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	user, err := d.client.GetUser(ctx, id)
	if client.IsForbidden(err) {
		d.readSummary(ctx, id, err, resp)
		return
	}
	if err != nil {
		addUserReadError(&resp.Diagnostics, id, err)
		return
	}

	var state userDataSourceModel
	state.fromAPI(user)
	state.Header = readDataSourceHeader(ctx, d.client, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// readSummary reports what a key that is not an admin's may learn about
// another user, whose details Homarr refused it: the entry GET
// /api/users/selectable holds for the user, and the auth provider it is listed
// under. Homarr refuses such a key before it even checks that the user exists,
// so a user missing from the list does not exist.
func (d *userDataSource) readSummary(ctx context.Context, id string, refused error, resp *datasource.ReadResponse) {
	users, err := d.client.ListUsersByProvider(ctx)
	if err != nil {
		addUserReadError(&resp.Diagnostics, id, refused)
		return
	}
	index := slices.IndexFunc(users, func(u client.ProviderUser) bool { return u.ID == id })
	if index < 0 {
		resp.Diagnostics.AddError("Homarr user not found", "Homarr has no user with id "+id+".")
		return
	}
	user := users[index]

	state := userDataSourceModel{
		ID:       types.StringValue(user.ID),
		Username: stringValueOrNull(user.Name),
		Provider: types.StringValue(user.Provider),
		Image:    stringValueOrNull(user.Image),
	}
	state.Email = emailValueOrNull(user.Email)
	state.Header = types.ObjectNull(headerAttrTypes)

	resp.Diagnostics.AddWarning(
		"Only the summary of the Homarr user is available",
		"The API key is not an admin, so Homarr shows it the details of no user but its own. The id, "+
			"username, auth provider, email and image of user "+id+" come from GET /api/users/selectable, "+
			"which any key may read; its preferences and header are null. Use an admin key to read them. "+
			refused.Error(),
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// userIDByName resolves a lookup by username, optionally narrowed to one auth
// provider, to the id of the single user it names.
//
// The users are listed through GET /api/users/selectable rather than
// GET /api/users: it is open to any API key, so a key that is not an admin's
// can still find its own user by name, and it filters by provider on the
// server, which spares reading every user of that name to learn its provider.
func userIDByName(ctx context.Context, c *client.Client, name, provider string, diags *diag.Diagnostics) string {
	if name == "" {
		// The configuration validators rule this out; they only skip their
		// check while a value is still unknown.
		diags.AddError("Missing user lookup key", "Set either `id` or `username` to identify the user.")
		return ""
	}

	users, err := c.FindUsersByName(ctx, name, provider)
	if err != nil {
		diags.AddError("Unable to look up the Homarr user", err.Error())
		return ""
	}
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	slices.Sort(ids)

	switch {
	case len(ids) == 1:
		return ids[0]
	case len(ids) == 0 && provider == "":
		diags.AddAttributeError(
			path.Root("username"),
			"Homarr user not found",
			fmt.Sprintf("No Homarr user is named %q, ignoring case.", name),
		)
	case len(ids) == 0:
		diags.AddAttributeError(
			path.Root("username"),
			"Homarr user not found",
			fmt.Sprintf("No Homarr user signing in through %s is named %q, ignoring case.", provider, name),
		)
	case provider == "":
		diags.AddAttributeError(
			path.Root("username"),
			"Ambiguous Homarr user lookup",
			fmt.Sprintf("%d users are named %q (ids %s). Homarr keeps usernames unique only per auth provider, "+
				"so an LDAP or OIDC account can share its name with another account. Set provider_name to "+
				"credentials, oidc or ldap to pick one, or look the user up by id.",
				len(ids), name, strings.Join(ids, ", ")),
		)
	default:
		diags.AddAttributeError(
			path.Root("username"),
			"Ambiguous Homarr user lookup",
			fmt.Sprintf("%d users signing in through %s are named %q apart from case (ids %s), which Homarr "+
				"releases from before it lowercased usernames allowed. Look the user up by id.",
				len(ids), provider, name, strings.Join(ids, ", ")),
		)
	}
	return ""
}

var (
	_ datasource.DataSource              = &currentUserDataSource{}
	_ datasource.DataSourceWithConfigure = &currentUserDataSource{}
)

// NewCurrentUserDataSource returns the homarr_current_user data source.
func NewCurrentUserDataSource() datasource.DataSource { return &currentUserDataSource{} }

type currentUserDataSource struct {
	client *client.Client
}

func (d *currentUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_current_user"
}

func (d *currentUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reports the Homarr user the provider's API key belongs to, with the same " +
			"attributes as the `homarr_user` data source. Any API key may read it; no admin is needed.\n\n" +
			"Homarr has no \"who am I\" endpoint. `GET /api/users/preferences` without a user id answers for " +
			"the caller and names it, and `GET /api/users/{id}` then reads the account, which every user may do " +
			"for themselves.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer**, which added `GET /api/users/preferences`. Older releases " +
			"offer no way to learn which user an API key belongs to.",
		Attributes: userDataSourceAttributes(false),
	}
}

func (d *currentUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *currentUserDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	// The preferences come from this call too, header included, so the account
	// read that follows is the only other request.
	prefs, err := d.client.GetUserPreferences(ctx, "")
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"Unable to identify the API key's Homarr user",
				"homarr_current_user requires Homarr 2.3.0 or newer: GET /api/users/preferences answered 404, "+
					"so this Homarr predates it. "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to identify the API key's Homarr user", err.Error())
		return
	}
	if prefs.UserID == "" {
		resp.Diagnostics.AddError(
			"Unable to identify the API key's Homarr user",
			"GET /api/users/preferences did not report the id of the API key's user.",
		)
		return
	}

	user, err := d.client.GetUser(ctx, prefs.UserID)
	if err != nil {
		addUserReadError(&resp.Diagnostics, prefs.UserID, err)
		return
	}

	var state userDataSourceModel
	state.fromAPI(user)
	state.userPreferenceValues.fromAPI(prefs.UserPreferenceFields)
	state.Header = headerDataSourceValue(prefs.HeaderPreferences)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
