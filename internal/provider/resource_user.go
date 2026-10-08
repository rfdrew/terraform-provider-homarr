package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ resource.Resource                = &userResource{}
	_ resource.ResourceWithConfigure   = &userResource{}
	_ resource.ResourceWithImportState = &userResource{}
)

// NewUserResource returns the homarr_user resource.
func NewUserResource() resource.Resource { return &userResource{} }

type userResource struct {
	client *client.Client
}

type userModel struct {
	ID                types.String `tfsdk:"id"`
	Username          types.String `tfsdk:"username"`
	Password          types.String `tfsdk:"password"`
	Email             types.String `tfsdk:"email"`
	GroupIDs          types.List   `tfsdk:"group_ids"`
	HomeBoardID       types.String `tfsdk:"home_board_id"`
	MobileHomeBoardID types.String `tfsdk:"mobile_home_board_id"`
	Provider          types.String `tfsdk:"provider_name"`

	ColorScheme               types.String `tfsdk:"color_scheme"`
	ByteUnitSystem            types.String `tfsdk:"byte_unit_system"`
	FirstDayOfWeek            types.Int64  `tfsdk:"first_day_of_week"`
	PingIconsEnabled          types.Bool   `tfsdk:"ping_icons_enabled"`
	EnableRightClickOnWidgets types.Bool   `tfsdk:"enable_right_click_on_widgets"`
	DefaultSearchEngineID     types.String `tfsdk:"default_search_engine_id"`
	OpenSearchInNewTab        types.Bool   `tfsdk:"open_search_in_new_tab"`
	DdgBangs                  types.Bool   `tfsdk:"ddg_bangs"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr user account authenticated with credentials.\n\n" +
			"Requires an API key belonging to an admin, and Homarr must have the `credentials` auth provider " +
			"enabled — accounts backed by LDAP or OIDC cannot be created this way. To manage the preferences " +
			"of an account this resource did not create, use `homarr_user_preferences`.\n\n" +
			"~> **The preference attributes require Homarr 2.3.0 or newer**, which added " +
			"`PATCH /api/users/preferences`. They are read from `GET /api/users/{id}` and so are reported on " +
			"older releases too, but setting them needs 2.3.0; the home board attributes fall back to the " +
			"older endpoint automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Homarr-generated identifier of the user. Homarr's create endpoint returns " +
					"no body, so the provider recovers this by looking the username up immediately afterwards.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Login name, between 3 and 255 characters. Homarr lowercases usernames, so " +
					"the value stored in state may differ in case from the configuration. Renaming is not " +
					"supported by the API and forces a new user.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(3, 255),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"password": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				MarkdownDescription: "Account password, at least 8 characters. Homarr's UI additionally asks for " +
					"lowercase, uppercase, digit and special characters; the API enforces only the length.\n\n" +
					"Changing this value calls Homarr's change-password endpoint. Because the provider cannot " +
					"read a password back, a password changed outside Terraform is not detected as drift.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(8, 255),
				},
			},
			"email": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Optional email address. Must be a valid address; Homarr rejects an " +
					"explicit null, so leave the attribute out entirely when the user has no email.\n\n" +
					"~> **Note** Changing this forces a new user. Homarr's only email-editing endpoint acts on " +
					"the account behind the API key, so there is no way to change a managed user's email in " +
					"place.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"group_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				MarkdownDescription: "Identifiers of groups the user joins at creation.\n\n" +
					"~> **Note** Group membership is create-only here. Homarr exposes group management solely " +
					"over tRPC, so the provider can neither read memberships back nor change them later; " +
					"changing this list forces a new user.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown to this user on desktop. The user must be able to view the " +
					"board, so it generally has to be public or explicitly shared.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Board shown to this user on mobile.",
			},
			"provider_name": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Auth provider backing the account, as reported by Homarr. Always " +
					"`credentials` for users created by this provider.",
			},
			"color_scheme": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Interface colour scheme. One of `auto`, `light` or `dark`.",
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "light", "dark"),
				},
			},
			"byte_unit_system": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "How byte sizes are displayed. One of `binary` (KiB, MiB) or " +
					"`decimal` (kB, MB).",
				Validators: []validator.String{
					stringvalidator.OneOf("binary", "decimal"),
				},
			},
			"first_day_of_week": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "First day of the week in calendar widgets, `0` for Sunday through " +
					"`6` for Saturday.",
				Validators: []validator.Int64{
					int64validator.Between(0, 6),
				},
			},
			"ping_icons_enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Show online-status ping indicators as icons rather than dots.",
			},
			"enable_right_click_on_widgets": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Let widgets handle right-clicks instead of the browser context menu.",
			},
			"default_search_engine_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Identifier of the search engine used by the search bar. Search engines " +
					"themselves have no REST surface, so the id has to come from Homarr.",
			},
			"open_search_in_new_tab": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Open search results in a new browser tab.",
			},
			"ddg_bangs": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Enable DuckDuckGo-style `!bang` shortcuts in the search bar.",
			},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies readable fields into the model. password and group_ids are
// untouched: neither can be read back from Homarr.
func (m *userModel) fromAPI(user *client.UserDetail) {
	m.ID = types.StringValue(user.ID)
	m.Username = types.StringValue(user.Name)
	m.Provider = types.StringValue(user.Provider)
	m.HomeBoardID = stringValueOrNull(user.HomeBoardID)
	m.MobileHomeBoardID = stringValueOrNull(user.MobileHomeBoardID)

	// Homarr stores an omitted email as null but a supplied empty string as "".
	// Normalising "" to null here would fight the configuration, so the raw
	// value is kept.
	m.Email = stringValueOrNull(user.Email)

	// colorScheme and byteUnitSystem are absent on releases that predate them,
	// which decodes to "" rather than a real setting.
	m.ColorScheme = stringValueOrNullIfEmpty(user.ColorScheme)
	m.ByteUnitSystem = stringValueOrNullIfEmpty(user.ByteUnitSystem)
	m.FirstDayOfWeek = types.Int64Value(user.FirstDayOfWeek)
	m.PingIconsEnabled = types.BoolValue(user.PingIconsEnabled)
	m.EnableRightClickOnWidgets = types.BoolValue(user.EnableRightClickOnWidgets)
	m.DefaultSearchEngineID = stringValueOrNull(user.DefaultSearchEngineID)
	m.OpenSearchInNewTab = types.BoolValue(user.OpenSearchInNewTab)
	m.DdgBangs = types.BoolValue(user.DdgBangs)
}

// preferencesPatch collects every preference the plan sets that differs from
// the prior state. Pass a zero-valued prior to mean "everything the plan sets",
// which is what creation needs.
//
// Nullable references are written as an explicit null when the plan clears
// them, which is why the patch is a map: an omitted key and a null key mean
// different things to Homarr.
func preferencesPatch(plan, prior userModel) client.UserPreferencesPatch {
	patch := client.UserPreferencesPatch{}

	putString := func(key string, planned, previous types.String, nullable bool) {
		if planned.Equal(previous) || planned.IsUnknown() {
			return
		}
		switch {
		case !planned.IsNull():
			patch[key] = planned.ValueString()
		case nullable:
			patch[key] = nil
		}
	}
	putBool := func(key string, planned, previous types.Bool) {
		if planned.Equal(previous) || planned.IsUnknown() || planned.IsNull() {
			return
		}
		patch[key] = planned.ValueBool()
	}

	putString("homeBoardId", plan.HomeBoardID, prior.HomeBoardID, true)
	putString("mobileHomeBoardId", plan.MobileHomeBoardID, prior.MobileHomeBoardID, true)
	putString("defaultSearchEngineId", plan.DefaultSearchEngineID, prior.DefaultSearchEngineID, true)
	putString("colorScheme", plan.ColorScheme, prior.ColorScheme, false)
	putString("byteUnitSystem", plan.ByteUnitSystem, prior.ByteUnitSystem, false)
	putBool("pingIconsEnabled", plan.PingIconsEnabled, prior.PingIconsEnabled)
	putBool("enableRightClickOnWidgets", plan.EnableRightClickOnWidgets, prior.EnableRightClickOnWidgets)
	putBool("openSearchInNewTab", plan.OpenSearchInNewTab, prior.OpenSearchInNewTab)
	putBool("ddgBangs", plan.DdgBangs, prior.DdgBangs)

	if !plan.FirstDayOfWeek.Equal(prior.FirstDayOfWeek) && !plan.FirstDayOfWeek.IsUnknown() && !plan.FirstDayOfWeek.IsNull() {
		patch["firstDayOfWeek"] = plan.FirstDayOfWeek.ValueInt64()
	}
	return patch
}

// onlyHomeBoards reports whether a patch touches nothing but the two home
// board references, which the pre-2.3.0 endpoint can also express.
func onlyHomeBoards(patch client.UserPreferencesPatch) bool {
	for key := range patch {
		if key != "homeBoardId" && key != "mobileHomeBoardId" {
			return false
		}
	}
	return true
}

// applyPreferences sends one PATCH /api/users/preferences for everything that
// changed. Homarr validates the whole patch before writing, so this is atomic —
// which is why creation applies home boards and preferences together rather
// than in separate calls that could half-succeed.
//
// On a Homarr older than 2.3.0 that endpoint does not exist. When the patch is
// only about home boards the older single-purpose endpoint can still do the
// job, so it is used instead; anything else is reported as needing 2.3.0.
func (r *userResource) applyPreferences(ctx context.Context, id string, patch client.UserPreferencesPatch, diags *diag.Diagnostics) {
	if len(patch) == 0 {
		return
	}

	_, err := r.client.UpdateUserPreferences(ctx, id, patch)
	if err == nil {
		return
	}
	if !client.IsNotFound(err) {
		diags.AddError("Unable to update the Homarr user's preferences", err.Error())
		return
	}

	if !onlyHomeBoards(patch) {
		diags.AddError(
			"Unable to update the Homarr user's preferences",
			"PATCH /api/users/preferences is unavailable, which means this Homarr predates 2.3.0. "+
				"Only home boards can be managed on older releases; remove the other preference "+
				"attributes or upgrade Homarr. "+err.Error(),
		)
		return
	}

	home, _ := patch["homeBoardId"].(string)
	mobile, _ := patch["mobileHomeBoardId"].(string)
	var homePtr, mobilePtr *string
	if v, ok := patch["homeBoardId"]; ok && v != nil {
		homePtr = &home
	}
	if v, ok := patch["mobileHomeBoardId"]; ok && v != nil {
		mobilePtr = &mobile
	}
	if fallbackErr := r.client.SetUserHomeBoards(ctx, id, homePtr, mobilePtr); fallbackErr != nil {
		diags.AddError("Unable to set the Homarr user's home boards", fallbackErr.Error())
	}
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupIDs := []string{}
	if !plan.GroupIDs.IsNull() && !plan.GroupIDs.IsUnknown() {
		resp.Diagnostics.Append(plan.GroupIDs.ElementsAs(ctx, &groupIDs, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	password := plan.Password.ValueString()
	id, err := r.client.CreateUser(ctx, client.UserCreateRequest{
		Username:        plan.Username.ValueString(),
		Password:        password,
		ConfirmPassword: password,
		Email:           stringPointerOrNil(plan.Email),
		GroupIDs:        groupIDs,
	})
	if err != nil {
		if client.IsConflict(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("username"),
				"Username already taken",
				"Homarr already has a user with this name: "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to create Homarr user", err.Error())
		return
	}

	// Home boards and preferences go in one validated-then-written call, so a
	// freshly created user cannot end up half-configured.
	r.applyPreferences(ctx, id, preferencesPatch(plan, userModel{}), &resp.Diagnostics)
	// Fall through even on failure: the user exists and must reach state.

	user, err := r.client.GetUser(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr user back after creation", err.Error())
		return
	}

	plan.fromAPI(user)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.client.GetUser(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr user", err.Error())
		return
	}

	state.fromAPI(user)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	if !plan.Password.Equal(state.Password) {
		// The previous password is only actually verified when a user changes
		// their own password; passing the value from state makes that case work
		// too rather than only the admin-acting-on-others case.
		if err := r.client.ChangeUserPassword(ctx, id, state.Password.ValueString(), plan.Password.ValueString()); err != nil {
			resp.Diagnostics.AddError("Unable to change the Homarr user's password", err.Error())
			return
		}
	}

	r.applyPreferences(ctx, id, preferencesPatch(plan, state), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	user, err := r.client.GetUser(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr user back after update", err.Error())
		return
	}

	plan.fromAPI(user)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteUser(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete Homarr user", err.Error())
	}
}

// ImportState imports a user by id.
//
// The password cannot be read from Homarr, so the first plan after import will
// propose a password change to whatever the configuration holds. That is
// harmless — it simply sets the password to the configured value.
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
