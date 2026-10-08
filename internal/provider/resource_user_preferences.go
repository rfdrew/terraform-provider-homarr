package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ resource.Resource                = &userPreferencesResource{}
	_ resource.ResourceWithConfigure   = &userPreferencesResource{}
	_ resource.ResourceWithImportState = &userPreferencesResource{}
)

// NewUserPreferencesResource returns the homarr_user_preferences resource.
func NewUserPreferencesResource() resource.Resource { return &userPreferencesResource{} }

type userPreferencesResource struct {
	client *client.Client
}

type userPreferencesModel struct {
	ID                        types.String `tfsdk:"id"`
	UserID                    types.String `tfsdk:"user_id"`
	ColorScheme               types.String `tfsdk:"color_scheme"`
	ByteUnitSystem            types.String `tfsdk:"byte_unit_system"`
	FirstDayOfWeek            types.Int64  `tfsdk:"first_day_of_week"`
	PingIconsEnabled          types.Bool   `tfsdk:"ping_icons_enabled"`
	EnableRightClickOnWidgets types.Bool   `tfsdk:"enable_right_click_on_widgets"`
	HomeBoardID               types.String `tfsdk:"home_board_id"`
	MobileHomeBoardID         types.String `tfsdk:"mobile_home_board_id"`
	DefaultSearchEngineID     types.String `tfsdk:"default_search_engine_id"`
	OpenSearchInNewTab        types.Bool   `tfsdk:"open_search_in_new_tab"`
	DdgBangs                  types.Bool   `tfsdk:"ddg_bangs"`
}

func (r *userPreferencesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_preferences"
}

func (r *userPreferencesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the preferences of an existing Homarr user.\n\n" +
			"`homarr_user` already carries these attributes for accounts it created. This resource exists " +
			"for the accounts it cannot create: users backed by LDAP or OIDC, the administrator made during " +
			"onboarding, and anyone added through the Homarr UI. It attaches to a user by id and never " +
			"creates or deletes the account itself.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer**, which added `GET` and `PATCH /api/users/preferences`.\n\n" +
			"~> **Do not manage the same user with both this resource and a `homarr_user`.** Both write the " +
			"same fields, so they would overwrite each other on every apply.\n\n" +
			"Every attribute is optional and computed: declare what you want to control and the rest is " +
			"refreshed from Homarr and left alone. Targeting a user other than the one behind the API key " +
			"requires admin. Destroying this resource leaves the preferences in place — they belong to the " +
			"user and disappear only with the account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Same value as `user_id`, present because Terraform requires an `id`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the user whose preferences these are.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
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
			"home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown to this user on desktop. The user must be able to view " +
					"it. Set to `null` to clear it.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Board shown to this user on mobile. Set to `null` to clear it.",
			},
			"default_search_engine_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Identifier of the search engine used by the search bar. Search " +
					"engines themselves have no REST surface, so the id has to come from Homarr.",
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

func (r *userPreferencesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m *userPreferencesModel) fromAPI(prefs *client.UserPreferences) {
	m.ID = types.StringValue(prefs.UserID)
	m.UserID = types.StringValue(prefs.UserID)
	m.ColorScheme = types.StringValue(prefs.ColorScheme)
	m.ByteUnitSystem = types.StringValue(prefs.ByteUnitSystem)
	m.FirstDayOfWeek = types.Int64Value(prefs.FirstDayOfWeek)
	m.PingIconsEnabled = types.BoolValue(prefs.PingIconsEnabled)
	m.EnableRightClickOnWidgets = types.BoolValue(prefs.EnableRightClickOnWidgets)
	m.HomeBoardID = stringValueOrNull(prefs.HomeBoardID)
	m.MobileHomeBoardID = stringValueOrNull(prefs.MobileHomeBoardID)
	m.DefaultSearchEngineID = stringValueOrNull(prefs.DefaultSearchEngineID)
	m.OpenSearchInNewTab = types.BoolValue(prefs.OpenSearchInNewTab)
	m.DdgBangs = types.BoolValue(prefs.DdgBangs)
}

// toPatch collects what the plan sets and the prior state does not already
// hold. Pass a zero-valued prior to mean "everything the plan sets".
func (m *userPreferencesModel) toPatch(prior userPreferencesModel) client.UserPreferencesPatch {
	return preferencesPatch(userModel{
		HomeBoardID:               m.HomeBoardID,
		MobileHomeBoardID:         m.MobileHomeBoardID,
		ColorScheme:               m.ColorScheme,
		ByteUnitSystem:            m.ByteUnitSystem,
		FirstDayOfWeek:            m.FirstDayOfWeek,
		PingIconsEnabled:          m.PingIconsEnabled,
		EnableRightClickOnWidgets: m.EnableRightClickOnWidgets,
		DefaultSearchEngineID:     m.DefaultSearchEngineID,
		OpenSearchInNewTab:        m.OpenSearchInNewTab,
		DdgBangs:                  m.DdgBangs,
	}, userModel{
		HomeBoardID:               prior.HomeBoardID,
		MobileHomeBoardID:         prior.MobileHomeBoardID,
		ColorScheme:               prior.ColorScheme,
		ByteUnitSystem:            prior.ByteUnitSystem,
		FirstDayOfWeek:            prior.FirstDayOfWeek,
		PingIconsEnabled:          prior.PingIconsEnabled,
		EnableRightClickOnWidgets: prior.EnableRightClickOnWidgets,
		DefaultSearchEngineID:     prior.DefaultSearchEngineID,
		OpenSearchInNewTab:        prior.OpenSearchInNewTab,
		DdgBangs:                  prior.DdgBangs,
	})
}

// apply sends the patch, if any, and refreshes the model from the response.
// Homarr returns the full preferences from PATCH, so no extra read is needed;
// when nothing changed it is read instead, to fill the computed attributes.
func (r *userPreferencesResource) apply(ctx context.Context, plan *userPreferencesModel, prior userPreferencesModel) error {
	userID := plan.UserID.ValueString()

	patch := plan.toPatch(prior)
	if len(patch) == 0 {
		prefs, err := r.client.GetUserPreferences(ctx, userID)
		if err != nil {
			return err
		}
		plan.fromAPI(prefs)
		return nil
	}

	prefs, err := r.client.UpdateUserPreferences(ctx, userID, patch)
	if err != nil {
		return err
	}
	plan.fromAPI(prefs)
	return nil
}

func (r *userPreferencesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userPreferencesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan, userPreferencesModel{}); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("user_id"),
				"User not found",
				"Homarr has no user with id "+plan.UserID.ValueString()+", or the API key is not an admin "+
					"and the id is not its own — Homarr answers 404 for both. On a Homarr older than "+
					"2.3.0 this endpoint does not exist at all. "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to apply Homarr user preferences", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userPreferencesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userPreferencesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	prefs, err := r.client.GetUserPreferences(ctx, state.UserID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// The user is gone, and so are its preferences.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr user preferences", err.Error())
		return
	}

	state.fromAPI(prefs)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userPreferencesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userPreferencesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan, state); err != nil {
		resp.Diagnostics.AddError("Unable to apply Homarr user preferences", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the resource from state without touching the server.
//
// Preferences are columns on the user row: there is nothing to delete, and
// Homarr's endpoint can only set values, never reset them to defaults.
func (r *userPreferencesResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Homarr user preferences were left unchanged",
		"homarr_user_preferences has been removed from Terraform state, but the preferences still apply to "+
			"the user. Homarr's API can only set them, never reset them to their defaults.",
	)
}

// ImportState imports by user id; Read then populates every attribute.
func (r *userPreferencesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), req.ID)...)
}
