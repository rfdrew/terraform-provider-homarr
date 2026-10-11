package provider

import (
	"context"
	"strings"

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
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Email    types.String `tfsdk:"email"`
	GroupIDs types.List   `tfsdk:"group_ids"`
	Provider types.String `tfsdk:"provider_name"`
	userPreferenceValues
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
			"older endpoint automatically.\n\n" +
			"The three references — `home_board_id`, `mobile_home_board_id` and `default_search_engine_id` — " +
			"are owned outright: leaving one out means the user has none, and a value set in the UI is " +
			"cleared on the next apply. The other preferences are optional and computed, so leaving them out " +
			"keeps whatever Homarr has.",
		Attributes: withUserPreferenceAttributes(map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Homarr-generated identifier of the user. Homarr's create endpoint returns " +
					"no body, so the provider recovers this by comparing the user list before and after the " +
					"create; an LDAP or OIDC account with the same name is never mistaken for the new user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"username": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Login name, between 3 and 255 characters (counted in UTF-16 code units, " +
					"as Homarr counts them). It must be lowercase and must not start or end with whitespace: " +
					"Homarr trims and lowercases every username before storing it, so any other spelling would " +
					"be stored as something other than what is configured. Renaming is not supported by the " +
					"API and forces a new user.\n\n" +
					"Older Homarr releases stored usernames as typed. To import such a user, first lowercase " +
					"the stored names with Homarr's `fix-usernames` CLI command.",
				Validators: []validator.String{
					usernameNormalisedValidator(),
					// usernameSchema checks the length after normalising, which
					// usernameNormalisedValidator requires to be a no-op.
					jsLengthBetweenValidator(3, 255),
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
					jsLengthBetweenValidator(8, 255),
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
				MarkdownDescription: "Board shown to this user on desktop. Leave it out, or set it to `null`, " +
					"for no home board. " + homeBoardAccessNote,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Board shown to this user on mobile. Leave it out, or set it to `null`, " +
					"for no mobile home board. " + homeBoardAccessNote,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"provider_name": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Auth provider backing the account, as reported by Homarr. Always " +
					"`credentials` for users created by this provider.",
			},
			"default_search_engine_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Identifier of the search engine used by the search bar. Search engines " +
					"themselves have no REST surface, so the id has to come from Homarr. Leave it out, or set " +
					"it to `null`, for none.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		}),
	}
}

// usernameNormalisedValidator rejects a username Homarr would rewrite. Its
// usernameSchema trims and lowercases the name before storing it, so that
// logins compare case-insensitively; a configured "TfUser" comes back as
// "tfuser". username is Required rather than Computed, so the provider cannot
// adopt the stored spelling: Terraform would reject the apply as an
// inconsistent result, and an import would plan a replacement on every run.
//
// strings.ToLower stands in for String.prototype.toLowerCase. The two can map
// a character to different results — JavaScript applies the full mappings,
// turning "İ" into "i" plus a combining dot — but they change exactly the same
// characters, and whether the value changes at all is the only question asked
// here.
func usernameNormalisedValidator() validator.String {
	return stringCheck{
		description: "must be lowercase with no leading or trailing whitespace, because Homarr trims and " +
			"lowercases usernames before storing them",
		check: func(s string) bool { return s == strings.ToLower(client.JSTrim(s)) },
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies readable fields into the model. password and group_ids are
// untouched: neither can be read back from Homarr.
func (m *userModel) fromAPI(user *client.UserDetail) {
	m.ID = types.StringValue(user.ID)
	// Homarr's stored name can be taken verbatim: the username validators only
	// admit a value Homarr stores unchanged, so it matches the configuration.
	m.Username = types.StringValue(user.Name)
	m.Provider = types.StringValue(user.Provider)

	// Homarr stores an omitted email as null but a supplied empty string as "".
	// Normalising "" to null here would fight the configuration, so the raw
	// value is kept.
	m.Email = stringValueOrNull(user.Email)

	m.userPreferenceValues.fromAPI(user.UserPreferenceFields)
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
	if isBoardNotFound(err) {
		// The endpoint exists and the user too; a home board was refused, and
		// the older endpoint would only refuse it again.
		addBoardNotFoundError(diags, patch, err)
		return
	}

	// Any other 404 means either that the user is gone or that this Homarr predates
	// 2.3.0 and has no such endpoint. Reading the user tells the two apart, and
	// also supplies the current home boards the older endpoint needs.
	user, userErr := r.client.GetUser(ctx, id)
	if userErr != nil {
		diags.AddError(
			"Unable to update the Homarr user's preferences",
			"PATCH /api/users/preferences answered 404, and the user could not be read either, so it may "+
				"have been deleted outside Terraform. "+userErr.Error(),
		)
		return
	}

	if !onlyHomeBoards(patch) {
		diags.AddError(
			"Unable to update the Homarr user's preferences",
			"The user exists but PATCH /api/users/preferences is unavailable, which means this Homarr "+
				"predates 2.3.0. Only home boards can be managed on older releases; remove the other "+
				"preference attributes or upgrade Homarr. "+err.Error(),
		)
		return
	}

	// PATCH /api/users/changeHome replaces both slots at once, so a slot the
	// patch does not mention keeps the value Homarr has now instead of being
	// sent as null and cleared.
	home, mobile := user.HomeBoardID, user.MobileHomeBoardID
	if v, ok := patch["homeBoardId"]; ok {
		home = patchStringPointer(v)
	}
	if v, ok := patch["mobileHomeBoardId"]; ok {
		mobile = patchStringPointer(v)
	}
	if fallbackErr := r.client.SetUserHomeBoards(ctx, id, home, mobile); fallbackErr != nil {
		diags.AddError("Unable to set the Homarr user's home boards", fallbackErr.Error())
	}
}

// patchStringPointer turns a patch value back into the pointer form the older
// endpoints take: a string becomes a pointer to it, and the explicit null that
// clears a reference becomes nil.
func patchStringPointer(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
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
	r.applyPreferences(ctx, id, preferencesPatch(plan.userPreferenceValues, userPreferenceValues{}), &resp.Diagnostics)
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

	r.applyPreferences(ctx, id, preferencesPatch(plan.userPreferenceValues, state.userPreferenceValues), &resp.Diagnostics)
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
