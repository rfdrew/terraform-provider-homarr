package provider

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
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
	_ resource.Resource                     = &userResource{}
	_ resource.ResourceWithConfigure        = &userResource{}
	_ resource.ResourceWithImportState      = &userResource{}
	_ resource.ResourceWithConfigValidators = &userResource{}
	_ resource.ResourceWithModifyPlan       = &userResource{}
	_ resource.ResourceWithIdentity         = &userResource{}
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
	PasswordWO        types.String `tfsdk:"password_wo"`
	PasswordWOVersion types.Int64  `tfsdk:"password_wo_version"`
	Email             types.String `tfsdk:"email"`
	GroupIDs          types.List   `tfsdk:"group_ids"`
	Provider          types.String `tfsdk:"provider_name"`
	Image             types.String `tfsdk:"image"`
	userPreferenceValues
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr user account authenticated with credentials.\n\n" +
			"Requires an API key belonging to an admin, and Homarr must have the `credentials` auth provider " +
			"enabled. Accounts backed by LDAP or OIDC can be neither created nor managed this way — Homarr " +
			"refuses to rename them or change their email or password — so importing one is an error. One " +
			"imported by an earlier version of the provider is still read, with a warning, and a plan that " +
			"would rename it or change its email or password is refused; remove it from state with a " +
			"`removed` block or `terraform state rm`. To manage the preferences of such an account, or of any " +
			"account this resource did not create, use `homarr_user_preferences`.\n\n" +
			"~> **The preference attributes require Homarr 2.3.0 or newer**, which added " +
			"`PATCH /api/users/preferences`. They are read from `GET /api/users/{id}` and so are reported on " +
			"older releases too, but setting them needs 2.3.0. The home boards and the search preferences — " +
			"`default_search_engine_id`, `open_search_in_new_tab` and `ddg_bangs` — fall back to older " +
			"endpoints automatically. `header` is only reported from 2.3.0 on.\n\n" +
			"The three references — `home_board_id`, `mobile_home_board_id` and `default_search_engine_id` — " +
			"are owned outright: leaving one out means the user has none, and a value set in the UI is " +
			"cleared on the next apply; to keep one, set it in the configuration or list it in the " +
			"`ignore_changes` lifecycle argument. The other preferences are optional and computed, so leaving " +
			"them out keeps whatever Homarr has.\n\n" +
			"~> **Destroying or replacing the user that owns the provider's API key is refused.** Homarr " +
			"deletes a user's API keys along with the account, which would cut Terraform off from Homarr in the " +
			"middle of the apply. To stop managing that account, remove it from state with a `removed` block " +
			"or `terraform state rm`. The check needs Homarr 2.3.0 or newer to tell which user owns the key.",
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
					"be stored as something other than what is configured.\n\n" +
					"Changing it renames the user in place through `PUT /api/users/profile`. Homarr refuses a " +
					"name another credentials account already holds, and keeps the user's sessions signed in.\n\n" +
					"Older Homarr releases stored usernames as typed. To import such a user, first lowercase " +
					"the stored names with Homarr's `fix-usernames` CLI command.",
				Validators: []validator.String{
					usernameNormalisedValidator(),
					// usernameSchema checks the length after normalising, which
					// usernameNormalisedValidator requires to be a no-op.
					jsLengthBetweenValidator(3, 255),
				},
			},
			"password": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Account password, at least 8 characters. Homarr's UI additionally asks for " +
					"lowercase, uppercase, digit and special characters; the API enforces only the length. " +
					"Exactly one of `password` and `password_wo` must be set.\n\n" +
					"Changing this value calls Homarr's change-password endpoint. Because the provider cannot " +
					"read a password back, a password changed outside Terraform is not detected as drift. The " +
					"value is kept in Terraform state; with Terraform 1.11 or newer, `password_wo` avoids that.",
				Validators: []validator.String{
					jsLengthBetweenValidator(8, 255),
				},
			},
			"password_wo": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				WriteOnly: true,
				MarkdownDescription: "Write-only alternative to `password`, never stored in the plan or state. " +
					"Requires Terraform 1.11 or newer. The same length rules apply, and exactly one of " +
					"`password` and `password_wo` must be set.\n\n" +
					"Terraform cannot compare a write-only value with the one applied before, so the password " +
					"is sent at creation and afterwards only when `password_wo_version` changes: change both " +
					"together to rotate it.\n\n" +
					"~> **Note** When the API key's own user changes its password, Homarr requires the " +
					"current one, which the provider only knows from `password` in state. To rotate that " +
					"account's password through `password_wo`, set the new value in the Homarr UI first; the " +
					"apply then confirms it.",
				Validators: []validator.String{
					jsLengthBetweenValidator(8, 255),
				},
			},
			"password_wo_version": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Version of `password_wo`, kept in state in place of the password itself. " +
					"Changing it — usually by incrementing it — sends the current `password_wo` to Homarr. Only " +
					"valid together with `password_wo`.",
				Validators: []validator.Int64{
					int64validator.AlsoRequires(path.MatchRoot("password_wo")),
				},
			},
			"email": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Optional email address. Homarr checks only its shape: 1 to 64 characters " +
					"before a single `@` and 1 to 255 after it, none of them whitespace. Leave the attribute " +
					"out when the user has no email.\n\n" +
					"Setting or changing it updates the user in place through `PUT /api/users/profile`. " +
					"Homarr can change an email but never clear one, so removing the attribute forces a new " +
					"user.\n\n" +
					"~> **Note** When Homarr runs with `AUTH_OIDC_ENABLE_DANGEROUS_CREDENTIALS_LINKING=true`, " +
					"an OIDC sign-in whose verified email equals this one signs in *as this user*. The email " +
					"therefore decides which OIDC identity can take the account over: only set an address " +
					"its owner controls.",
				Validators: []validator.String{
					emailValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						emailCleared,
						"Homarr cannot clear an email, so removing it requires a new user.",
						"Homarr cannot clear an email, so removing it requires a new user.",
					),
				},
			},
			"group_ids": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				MarkdownDescription: "Identifiers of groups the user joins at creation. Each must be unique and " +
					"non-empty: Homarr adds the memberships only after creating the account, so a duplicate or " +
					"unknown group fails the create with the user already in place. The provider records such " +
					"a user as tainted, and the next apply replaces it.\n\n" +
					"~> **Note** Group membership is create-only here. Homarr exposes group management solely " +
					"over tRPC, so the provider can neither read memberships back nor change them later; " +
					"adding or removing a group forces a new user. Reordering the list does not.\n\n" +
					"~> **Leave `group_ids` out of imported users.** After an import the provider knows of no " +
					"memberships, so a configured `group_ids` plans a replacement, which deletes and recreates " +
					"the account along with its API keys and sessions.",
				Validators: []validator.List{
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplaceIf(
						groupMembershipChanged,
						"Group membership is create-only, so adding or removing a group requires a new user.",
						"Group membership is create-only, so adding or removing a group requires a new user.",
					),
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
				MarkdownDescription: "Auth provider backing the account, as reported by Homarr: `credentials`, " +
					"since the resource refuses to import any other account. Only a user imported by an " +
					"earlier version of the provider can report `ldap` or `oidc`.",
			},
			"image": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Profile picture of the user, as a base64 `data:` URL of a PNG, JPEG, GIF or " +
					"WebP image, at most 350,000 characters long — roughly 256 KiB of picture — for instance " +
					"`\"data:image/png;base64,${filebase64(\"avatar.png\")}\"`. Homarr accepts no other form, " +
					"not even an `https` URL.\n\n" +
					"Leave it out to keep whatever Homarr has, such as a picture the user uploaded in the UI; set " +
					"it to `\"\"` to remove the picture. It is set through `PUT /api/users/profileImage`, which " +
					"every Homarr 2.x release has. The whole value is kept in Terraform state.",
				Validators: []validator.String{
					profileImageValidator{},
				},
				PlanModifiers: []planmodifier.String{
					// A picture changes only when configured to, so an unconfigured
					// one is planned as it is. A null one is left unknown instead:
					// state written before the attribute existed holds null even
					// for a user with a picture.
					stringplanmodifier.UseNonNullStateForUnknown(),
				},
			},
			"default_search_engine_id": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Identifier of the search engine used by the search bar. Search engines " +
					"themselves have no REST surface, so the id has to come from Homarr. Leave it out, or set " +
					"it to `null`, for none.\n\n" +
					"Like the home boards, it is owned outright: a search engine the user picks in the UI is " +
					"cleared on the next apply unless it is configured here. Configurations written for provider " +
					"0.2.x lack the attribute, so the first apply after upgrading clears such a choice.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
		}),
	}
}

// ConfigValidators requires exactly one way of supplying the password.
// password_wo is visible here although it never reaches the plan: validation
// runs against the configuration.
func (r *userResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("password"), path.MatchRoot("password_wo")),
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

func (r *userResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema("id", "Homarr-generated identifier of the user.")
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies readable fields into the model. password, password_wo_version
// and group_ids are untouched: none of them can be read back from Homarr. The
// header is read separately, by readHeader.
func (m *userModel) fromAPI(user *client.UserDetail) {
	m.ID = types.StringValue(user.ID)
	// "" asks for no picture, which Homarr stores as null.
	m.Image = stringValueOrNullKeepingEmpty(user.Image, m.Image)
	// Homarr's stored name can be taken verbatim: the username validators only
	// admit a value Homarr stores unchanged, so it matches the configuration.
	// The column is nullable, though no credentials account lacks a name.
	m.Username = stringValueOrNull(user.Name)
	m.Provider = types.StringValue(user.Provider)

	// Homarr stores an omitted email as null but one supplied as "" verbatim.
	// Both mean the user has none, and the email validator rejects "" in
	// configuration, so reading it as null cannot fight the configuration;
	// keeping "" would instead plan a replacement to drop it.
	m.Email = emailValueOrNull(user.Email)

	m.userPreferenceValues.fromAPI(user.UserPreferenceFields)
}

// The patch keys that releases before 2.3.0 can still write, grouped by the
// endpoint that writes them: PATCH /api/users/changeHome takes both home
// boards, and PATCH /api/users/search-preferences the three search
// preferences. Each endpoint replaces all of its fields at once.
var (
	homeBoardPatchKeys        = []string{"homeBoardId", "mobileHomeBoardId"}
	searchPreferencePatchKeys = []string{"defaultSearchEngineId", "openSearchInNewTab", "ddgBangs"}
)

// preferencePatchAttributes names the attribute behind each patch key, for
// diagnostics.
var preferencePatchAttributes = map[string]string{
	"homeBoardId":               "home_board_id",
	"mobileHomeBoardId":         "mobile_home_board_id",
	"defaultSearchEngineId":     "default_search_engine_id",
	"colorScheme":               "color_scheme",
	"byteUnitSystem":            "byte_unit_system",
	"firstDayOfWeek":            "first_day_of_week",
	"pingIconsEnabled":          "ping_icons_enabled",
	"enableRightClickOnWidgets": "enable_right_click_on_widgets",
	"openSearchInNewTab":        "open_search_in_new_tab",
	"ddgBangs":                  "ddg_bangs",
	"headerPreferences":         "header",
}

// preferencesNeedingCurrentAPI returns, sorted, the attributes behind the patch
// keys that no endpoint older than 2.3.0 can write.
func preferencesNeedingCurrentAPI(patch client.UserPreferencesPatch) []string {
	var attributes []string
	for key := range patch {
		if slices.Contains(homeBoardPatchKeys, key) || slices.Contains(searchPreferencePatchKeys, key) {
			continue
		}
		attribute, ok := preferencePatchAttributes[key]
		if !ok {
			attribute = key
		}
		attributes = append(attributes, attribute)
	}
	sort.Strings(attributes)
	return attributes
}

// patchTouches reports whether the patch sets any of the keys.
func patchTouches(patch client.UserPreferencesPatch, keys []string) bool {
	for _, key := range keys {
		if _, ok := patch[key]; ok {
			return true
		}
	}
	return false
}

// applyPreferences sends one PATCH /api/users/preferences for everything that
// changed. Homarr validates the whole patch before writing, so this is atomic —
// which is why creation applies home boards and preferences together rather
// than in separate calls that could half-succeed.
//
// On a Homarr older than 2.3.0 that endpoint does not exist. The home boards
// and the three search preferences have older single-purpose endpoints, so a
// patch touching nothing else is sent through those instead; anything else is
// reported as needing 2.3.0. Without that fallback, a default search engine
// picked in the UI could never be cleared on those releases: the attribute is
// owned outright, so every plan would try to, and every apply would fail.
func (r *userResource) applyPreferences(ctx context.Context, id string, patch client.UserPreferencesPatch, diags *diag.Diagnostics) {
	if len(patch) == 0 {
		return
	}
	// The user may be the one behind the API key, whose home boards
	// homarr_board also writes; see homeBoardMu.
	if patchTouches(patch, homeBoardPatchKeys) {
		homeBoardMu.Lock()
		defer homeBoardMu.Unlock()
	}

	_, err := r.client.UpdateUserPreferences(ctx, id, patch)
	if err == nil {
		return
	}
	if isHeaderShortcutRefused(err) {
		addHeaderShortcutError(diags, err)
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
	// also supplies the current values the older endpoints need for the fields
	// the patch leaves alone.
	user, userErr := r.client.GetUser(ctx, id)
	if userErr != nil {
		diags.AddError(
			"Unable to update the Homarr user's preferences",
			"PATCH /api/users/preferences answered 404, and the user could not be read either, so it may "+
				"have been deleted outside Terraform. "+userErr.Error(),
		)
		return
	}

	if unsupported := preferencesNeedingCurrentAPI(patch); len(unsupported) > 0 {
		diags.AddError(
			"Unable to update the Homarr user's preferences",
			"The user exists but PATCH /api/users/preferences is unavailable, which means this Homarr "+
				"predates 2.3.0. Setting "+strings.Join(unsupported, ", ")+" requires Homarr 2.3.0 or newer; "+
				"only the home boards and the search preferences (default_search_engine_id, "+
				"open_search_in_new_tab and ddg_bangs) can be set on older releases. Upgrade Homarr, or "+
				"leave those attributes out — they are optional and computed, so an unconfigured one keeps "+
				"Homarr's value. "+err.Error(),
		)
		return
	}

	// Both older endpoints replace every field they take, so a field the patch
	// does not mention keeps the value Homarr has now instead of being sent as
	// null or false and overwritten.
	if patchTouches(patch, homeBoardPatchKeys) {
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
	if patchTouches(patch, searchPreferencePatchKeys) {
		engine, newTab, bangs := user.DefaultSearchEngineID, user.OpenSearchInNewTab, user.DdgBangs
		if v, ok := patch["defaultSearchEngineId"]; ok {
			engine = patchStringPointer(v)
		}
		if v, ok := patch["openSearchInNewTab"].(bool); ok {
			newTab = v
		}
		if v, ok := patch["ddgBangs"].(bool); ok {
			bangs = v
		}
		if fallbackErr := r.client.SetUserSearchPreferences(ctx, id, engine, newTab, bangs); fallbackErr != nil {
			diags.AddError("Unable to set the Homarr user's search preferences", fallbackErr.Error())
		}
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

	password, ok := configuredPassword(ctx, req.Config, plan, &resp.Diagnostics)
	if !ok {
		return
	}
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
				"Homarr already has a user with this name: "+err.Error()+"\n\nIf an earlier apply created this "+
					"user and failed before saving it to state, adopt it with an import block whose id is "+
					"\"username="+plan.Username.ValueString()+"\" instead of creating it again.",
			)
			return
		}
		if id != "" {
			// The account was created before the request failed. Recording it
			// taints the resource, so the next apply replaces it rather than
			// leaking it and failing on the name it holds.
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
			setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
			resp.Diagnostics.AddError("Unable to create Homarr user", err.Error()+"\n\n"+taintedUserNote)
			return
		}
		resp.Diagnostics.AddError("Unable to create Homarr user", err.Error())
		return
	}

	// Home boards and preferences go in one validated-then-written call, so a
	// freshly created user cannot end up half-configured.
	r.applyPreferences(ctx, id, preferencesPatch(plan.userPreferenceValues, userPreferenceValues{}), &resp.Diagnostics)
	// Fall through even on failure: the user exists and must reach state. A
	// new user has no picture, so only a configured one needs sending.
	if !plan.Image.IsNull() && !plan.Image.IsUnknown() && plan.Image.ValueString() != "" {
		r.applyImage(ctx, id, plan.Image, &resp.Diagnostics)
	}

	user, err := r.client.GetUser(ctx, id)
	if err != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
		resp.Diagnostics.AddError("Unable to read Homarr user back after creation", err.Error()+"\n\n"+taintedUserNote)
		return
	}

	plan.fromAPI(user)

	// Everything is applied by now, and only the layout is still to be read.
	// Failing the create over it would taint a healthy user, and the next apply
	// would replace it, so a failed read is only a warning: a configured layout
	// is the one just sent, an unconfigured one is left null, and the next
	// refresh reads it.
	var headerDiags diag.Diagnostics
	readHeader(ctx, r.client, id, &plan.userPreferenceValues, &headerDiags)
	for _, d := range headerDiags {
		if d.Severity() == diag.SeverityError {
			resp.Diagnostics.AddWarning(d.Summary(), d.Detail()+"\n\nThe user was created regardless; the next "+
				"refresh reads the layout.")
			continue
		}
		resp.Diagnostics.Append(d)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
}

// applyImage sets the profile picture a plan asks for: "" removes it.
func (r *userResource) applyImage(ctx context.Context, id string, image types.String, diags *diag.Diagnostics) {
	var value *string
	if image.ValueString() != "" {
		value = stringPointerOrNil(image)
	}
	if err := r.client.SetUserProfileImage(ctx, id, value); err != nil {
		diags.AddAttributeError(path.Root("image"), "Unable to set the Homarr user's profile picture", err.Error())
	}
}

// taintedUserNote explains a create that failed after the account came into
// being, for the error that reports it.
const taintedUserNote = "The user exists in Homarr regardless. Its id has been saved to Terraform state with the " +
	"resource marked as tainted, so the next apply deletes and recreates it instead of failing on the taken name."

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	// Before the lookup, so that a refresh finding the user gone still reports
	// one; see setIdentity.
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)

	user, err := r.client.GetUser(ctx, id)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr user", err.Error())
		return
	}
	if !isCredentialsUser(user) {
		addExternalUserWarning(&resp.Diagnostics, user)
	}

	state.fromAPI(user)
	readHeader(ctx, r.client, id, &state.userPreferenceValues, &resp.Diagnostics)
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

	// The profile goes first: the rename is the change most likely to be
	// refused, on a name another account holds, and nothing else has been
	// written by then. The password, the preferences and the picture follow in
	// that order.
	if !plan.Username.Equal(state.Username) || !plan.Email.Equal(state.Email) {
		// Homarr requires the name even when only the email changes. A null
		// email is omitted and leaves the stored one alone; clearing one is a
		// replacement, planned by emailCleared, so it never reaches this call.
		err := r.client.UpdateUserProfile(ctx, id, plan.Username.ValueString(), stringPointerOrNil(plan.Email))
		if err != nil {
			if client.IsConflict(err) {
				resp.Diagnostics.AddAttributeError(
					path.Root("username"),
					"Username already taken",
					"Homarr already has a credentials user with this name: "+err.Error(),
				)
				return
			}
			resp.Diagnostics.AddError("Unable to update the Homarr user's username and email", err.Error())
			return
		}
	}

	if newPassword, ok := passwordChange(ctx, req.Config, plan, state, &resp.Diagnostics); ok {
		// The previous password is only actually verified when a user changes
		// their own password; passing the value from state makes that case work
		// too rather than only the admin-acting-on-others case.
		err := r.client.ChangeUserPassword(ctx, id, state.Password.ValueString(), newPassword)
		if isInvalidPreviousPassword(err) {
			// State holds no password after an import or with password_wo, and a
			// stale one after a change made in the UI. If that change already set
			// the configured password, it is also the previous one; trying it
			// lets such an apply converge instead of failing on every run.
			err = r.client.ChangeUserPassword(ctx, id, newPassword, newPassword)
		}
		if err != nil {
			resp.Diagnostics.AddError("Unable to change the Homarr user's password", passwordChangeErrorDetail(err))
			return
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	r.applyPreferences(ctx, id, preferencesPatch(plan.userPreferenceValues, state.userPreferenceValues), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Image.IsNull() && !plan.Image.IsUnknown() && !plan.Image.Equal(state.Image) {
		r.applyImage(ctx, id, plan.Image, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	user, err := r.client.GetUser(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr user back after update", err.Error())
		return
	}

	plan.fromAPI(user)
	readHeader(ctx, r.client, id, &plan.userPreferenceValues, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
}

// ModifyPlan refuses two kinds of plan that are bound to fail or to do harm:
// changing the username, email or password of an LDAP or OIDC account, which
// Homarr answers with a 403, and deleting the user that owns the provider's API
// key, whether by destroying or by replacing it. Before that, it turns a plan
// that changes nothing back into the prior state: see keepUnchangedPlan.
//
// The owner check is only a courtesy at plan time; Delete checks again, and
// anything that keeps the check from answering here is left to it.
func (r *userResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() {
		return
	}
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if req.Plan.Raw.IsNull() {
		if r.ownsAPIKey(ctx, state.ID.ValueString()) {
			addAPIKeyOwnerError(&resp.Diagnostics, state.ID.ValueString())
		}
		return
	}

	keepUnchangedPlan(req, resp)
	var plan userModel
	resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if replacesUser(plan, state) {
		if r.ownsAPIKey(ctx, state.ID.ValueString()) {
			addAPIKeyOwnerReplacementError(&resp.Diagnostics, state.ID.ValueString())
		}
		return
	}
	refuseExternalAccountChanges(plan, state, &resp.Diagnostics)
}

// ownsAPIKey reports whether id is known to be the user the provider's API key
// belongs to. It is false whenever that cannot be told, including when the
// provider is not configured yet.
func (r *userResource) ownsAPIKey(ctx context.Context, id string) bool {
	if r.client == nil || id == "" {
		return false
	}
	owner, err := r.client.CurrentUserID(ctx)
	return err == nil && owner == id
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	r.refuseAPIKeyOwner(ctx, id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteUser(ctx, id); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete Homarr user", err.Error())
	}
}

// ImportState imports a user by id, or by name as "username=<name>"; from
// Terraform 1.12 also by the id in an import block's identity. Every way, an
// LDAP or OIDC account is refused.
//
// The password cannot be read from Homarr, so the first plan after import will
// propose a password change to whatever the configuration holds. That is
// harmless — it simply sets the password to the configured value. With
// password_wo, the version has to be set for that first change to happen.
//
// Group memberships cannot be read either, so group_ids stays null, and a
// configured one plans a replacement; the group_ids description warns of that.
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, byIdentity := importedID(ctx, req, "id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if name, byName := strings.CutPrefix(id, "username="); byName && !byIdentity {
		id = r.credentialsUserIDByName(ctx, name, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	} else if user, err := r.client.GetUser(ctx, id); err == nil && !isCredentialsUser(user) {
		// Any error is left to Read, which runs next and reports it — or, for
		// a user that does not exist, lets Terraform say so.
		addExternalUserImportError(&resp.Diagnostics, user.ID, user.Provider)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
}
