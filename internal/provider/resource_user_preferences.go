package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.ResourceWithIdentity    = &userPreferencesResource{}
	_ resource.ResourceWithModifyPlan  = &userPreferencesResource{}
)

// NewUserPreferencesResource returns the homarr_user_preferences resource.
func NewUserPreferencesResource() resource.Resource { return &userPreferencesResource{} }

type userPreferencesResource struct {
	client *client.Client
}

type userPreferencesModel struct {
	ID     types.String `tfsdk:"id"`
	UserID types.String `tfsdk:"user_id"`
	Image  types.String `tfsdk:"image"`
	userPreferenceValues
}

func (r *userPreferencesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_preferences"
}

func (r *userPreferencesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the preferences of an existing Homarr user.\n\n" +
			"`homarr_user` already carries these attributes for accounts it created. This resource exists " +
			"for the accounts it cannot create: users backed by LDAP or OIDC, the administrator made during " +
			"onboarding, and anyone added through the Homarr UI. It attaches to a user by id — or, with " +
			"`user_id` left out, to the user behind the API key — and never creates or deletes the account " +
			"itself.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer**, which added `GET` and `PATCH /api/users/preferences`.\n\n" +
			"~> **Do not manage the same user with both this resource and a `homarr_user`.** Both write the " +
			"same fields, so they would overwrite each other on every apply.\n\n" +
			"Every attribute is optional and computed: declare what you want to control and the rest is " +
			"refreshed from Homarr and left alone. Because of that, `null` means \"not managed\" rather than " +
			"\"empty\": to clear `home_board_id`, `mobile_home_board_id` or `default_search_engine_id`, or to " +
			"remove the `image`, set it to `\"\"`. Targeting a user other than the one behind the API key " +
			"requires admin. Destroying this resource leaves the preferences and the picture in place — they " +
			"belong to the user and disappear only with the account.",
		Attributes: withUserPreferenceAttributes(map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Same value as `user_id`, present because Terraform requires an `id`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Identifier of the user whose preferences these are; the `homarr_user` " +
					"data source finds it by username. Leave it out to target the user behind the API key, " +
					"which any key may do — handy for a non-admin key managing its own preferences without " +
					"looking up its id.\n\n" +
					"The user is resolved at creation and the resource stays attached to it, even if the " +
					"provider is later configured with another user's key. Setting `user_id` to a different " +
					"user forces a new resource. Removing a configured `user_id` later does not retarget the " +
					"resource either: it stays on that user. To manage the API key's own user instead, use " +
					"`terraform apply -replace` or a new resource.",
				PlanModifiers: []planmodifier.String{
					// Left out, the id comes from Homarr at creation and is kept
					// from then on. Only a configured id can ask for another user;
					// a plain RequiresReplace would also fire whenever the omitted
					// id is planned as unknown.
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown to this user on desktop. Set to `\"\"` to clear it; leaving " +
					"it out keeps whatever Homarr has. " + homeBoardAccessNote,
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Board shown to this user on mobile. Set to `\"\"` to clear it; leaving " +
					"it out keeps whatever Homarr has. " + homeBoardAccessNote,
			},
			"default_search_engine_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Identifier of the search engine used by the search bar. Search " +
					"engines themselves have no REST surface, so the id has to come from Homarr. Set to `\"\"` " +
					"to clear it; leaving it out keeps whatever Homarr has.",
			},
			"image": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Profile picture of the user, as a base64 `data:` URL of a PNG, JPEG, GIF or " +
					"WebP image, at most 350,000 characters long — for instance " +
					"`\"data:image/png;base64,${filebase64(\"avatar.png\")}\"`. Set to `\"\"` to remove the " +
					"picture; leaving it out keeps whatever Homarr has. Unlike a rename, Homarr sets a picture for " +
					"LDAP and OIDC accounts too, through `PUT /api/users/profileImage`, and reports it from " +
					"`GET /api/users/{id}`. The whole value is kept in Terraform state.\n\n" +
					"~> **Note** At every sign-in of an OIDC account, Homarr replaces a picture that is not a " +
					"`data:` URL with the one the identity provider supplies. A picture set here therefore stays, " +
					"while one removed with `\"\"` comes back at the user's next sign-in and shows up as drift.",
				Validators: []validator.String{
					profileImageValidator{},
				},
				PlanModifiers: []planmodifier.String{
					// As on homarr_user: an unconfigured picture is planned as it
					// is, unless state holds none, as it does when written before
					// the attribute existed.
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
		}),
	}
}

// IdentitySchema names the preferences by user_id, which is known once they
// are created even when the configuration left it out; id only repeats it.
func (r *userPreferencesResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema("user_id", "Identifier of the user whose preferences these are.")
}

func (r *userPreferencesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (m *userPreferencesModel) fromAPI(prefs *client.UserPreferences) {
	m.ID = types.StringValue(prefs.UserID)
	m.UserID = types.StringValue(prefs.UserID)
	m.userPreferenceValues.fromAPI(prefs.UserPreferenceFields)
	m.headerFromAPI(prefs.HeaderPreferences)
}

// apply sends the patch, if any, and refreshes the model from the response.
// Homarr returns the full preferences from PATCH, so no extra read is needed;
// when nothing changed it is read instead, to fill the computed attributes.
//
// An omitted user_id is unknown at creation, which sends no userId: Homarr then
// targets the API key's own user and reports its id in the response.
func (r *userPreferencesResource) apply(ctx context.Context, plan *userPreferencesModel, patch client.UserPreferencesPatch) error {
	userID := ""
	if !plan.UserID.IsUnknown() {
		userID = plan.UserID.ValueString()
	}

	if len(patch) == 0 {
		prefs, err := r.client.GetUserPreferences(ctx, userID)
		if err != nil {
			return err
		}
		plan.fromAPI(prefs)
		return nil
	}

	// The user may be the one behind the API key, whose home boards
	// homarr_board also writes; see homeBoardMu.
	if patchTouches(patch, homeBoardPatchKeys) {
		homeBoardMu.Lock()
		defer homeBoardMu.Unlock()
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

	patch := preferencesPatch(plan.userPreferenceValues, userPreferenceValues{})
	if err := r.apply(ctx, &plan, patch); err != nil {
		if isBoardNotFound(err) {
			addBoardNotFoundError(&resp.Diagnostics, patch, err)
			return
		}
		if isHeaderShortcutRefused(err) {
			addHeaderShortcutError(&resp.Diagnostics, err)
			return
		}
		if client.IsNotFound(err) && plan.UserID.IsUnknown() {
			// The API key's own user always exists, so only the endpoint
			// itself can be missing.
			resp.Diagnostics.AddError(
				"Unable to apply Homarr user preferences",
				"homarr_user_preferences requires Homarr 2.3.0 or newer: /api/users/preferences answered "+
					"404, so this Homarr predates it. "+err.Error(),
			)
			return
		}
		if client.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("user_id"),
				"User not found",
				"Homarr has no user with id "+plan.UserID.ValueString()+", or the API key is not an admin "+
					"and the id is not its own — Homarr answers 404 for both. On a Homarr older than "+
					"2.3.0 this endpoint does not exist at all: homarr_user_preferences requires Homarr "+
					"2.3.0 or newer. "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to apply Homarr user preferences", err.Error())
		return
	}

	r.applyImage(ctx, &plan, types.StringNull(), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "user_id", plan.UserID.ValueString(), &resp.Diagnostics)
}

// applyImage sets the profile picture when the plan asks for one that differs
// from prior — "" removes it — and fills in a picture the plan leaves to
// Homarr. Neither /api/users/preferences endpoint reports the picture, so it is
// read from GET /api/users/{id}. plan.UserID must be known by now.
func (r *userPreferencesResource) applyImage(ctx context.Context, plan *userPreferencesModel, prior types.String, diags *diag.Diagnostics) {
	userID := plan.UserID.ValueString()
	set := !plan.Image.IsNull() && !plan.Image.IsUnknown() && !plan.Image.Equal(prior)
	if set {
		var image *string
		if plan.Image.ValueString() != "" {
			image = stringPointerOrNil(plan.Image)
		}
		if err := r.client.SetUserProfileImage(ctx, userID, image); err != nil {
			diags.AddAttributeError(path.Root("image"), "Unable to set the Homarr user's profile picture", err.Error())
			return
		}
	}
	if !set && !plan.Image.IsUnknown() {
		return
	}
	user, err := r.client.GetUser(ctx, userID)
	if err != nil {
		diags.AddError("Unable to read the Homarr user's profile picture", err.Error())
		return
	}
	plan.Image = stringValueOrNullKeepingEmpty(user.Image, plan.Image)
}

func (r *userPreferencesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userPreferencesModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	userID := state.UserID.ValueString()
	// Before the lookup, so that a refresh finding the user gone still reports
	// one; see setIdentity.
	setIdentity(ctx, resp.Identity, "user_id", userID, &resp.Diagnostics)

	prefs, err := r.client.GetUserPreferences(ctx, userID)
	if err == nil {
		state.fromAPI(prefs)
		// The picture is not among the preferences Homarr reports.
		user, err := r.client.GetUser(ctx, userID)
		if client.IsNotFound(err) {
			// Deleted between the two reads, and its preferences with it.
			resp.State.RemoveResource(ctx)
			return
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to read the Homarr user's profile picture", err.Error())
			return
		}
		state.Image = stringValueOrNullKeepingEmpty(user.Image, state.Image)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	if !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to read Homarr user preferences", err.Error())
		return
	}

	// A 404 is ambiguous: Homarr answers it for a user that does not exist, for
	// another user when the key is not an admin, and — before 2.3.0 — for the
	// endpoint itself. Only the first means the resource is gone, so the user is
	// checked before dropping it from state. GET /api/users/{id} tells the three
	// apart: it 404s for a missing user, 403s for a non-admin key reading someone
	// else, and succeeds when only the preferences endpoint is missing.
	if _, userErr := r.client.GetUser(ctx, userID); userErr != nil {
		switch {
		case client.IsNotFound(userErr):
			// The user is gone, and so are its preferences.
			resp.State.RemoveResource(ctx)
		case client.IsForbidden(userErr):
			resp.Diagnostics.AddError(
				"API key cannot manage this Homarr user's preferences",
				"The API key is not an admin, so it can read and change only its own user's preferences, "+
					"not those of "+userID+". Use an admin key, or the key of that user. "+userErr.Error(),
			)
		default:
			resp.Diagnostics.AddError("Unable to verify the Homarr user still exists", userErr.Error())
		}
		return
	}

	resp.Diagnostics.AddWarning(
		"Homarr user preferences could not be refreshed",
		"The user exists, but GET /api/users/preferences answered 404, which means this Homarr predates "+
			"2.3.0. State was left as last applied, so drift is not detected.",
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userPreferencesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userPreferencesModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	patch := preferencesPatch(plan.userPreferenceValues, state.userPreferenceValues)
	if err := r.apply(ctx, &plan, patch); err != nil {
		if isBoardNotFound(err) {
			addBoardNotFoundError(&resp.Diagnostics, patch, err)
			return
		}
		if isHeaderShortcutRefused(err) {
			addHeaderShortcutError(&resp.Diagnostics, err)
			return
		}
		resp.Diagnostics.AddError("Unable to apply Homarr user preferences", err.Error())
		return
	}

	r.applyImage(ctx, &plan, state.Image, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "user_id", plan.UserID.ValueString(), &resp.Diagnostics)
}

// ModifyPlan turns a plan that changes nothing back into the prior state; see
// keepUnchangedPlan.
func (r *userPreferencesResource) ModifyPlan(_ context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	keepUnchangedPlan(req, resp)
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

// ImportState imports by user id, given either as the import id or, from
// Terraform 1.12, as the identity of an import block; Read then populates every
// attribute.
func (r *userPreferencesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	userID, _ := importedID(ctx, req, "user_id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), userID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
	setIdentity(ctx, resp.Identity, "user_id", userID, &resp.Diagnostics)
}
