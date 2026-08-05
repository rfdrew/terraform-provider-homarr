package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr user account authenticated with credentials.\n\n" +
			"Requires an API key belonging to an admin, and Homarr must have the `credentials` auth provider " +
			"enabled — accounts backed by LDAP or OIDC cannot be created this way.",
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
				MarkdownDescription: "Board shown to this user on desktop. The user must be able to view the " +
					"board, so it generally has to be public or explicitly shared.",
			},
			"mobile_home_board_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Board shown to this user on mobile.",
			},
			"provider_name": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Auth provider backing the account, as reported by Homarr. Always " +
					"`credentials` for users created by this provider.",
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

	// Home boards are a separate endpoint, applied only when requested.
	if !plan.HomeBoardID.IsNull() || !plan.MobileHomeBoardID.IsNull() {
		if err := r.client.SetUserHomeBoards(ctx, id, stringPointerOrNil(plan.HomeBoardID), stringPointerOrNil(plan.MobileHomeBoardID)); err != nil {
			resp.Diagnostics.AddError(
				"Unable to set the Homarr user's home boards",
				"The user was created as "+id+" but its home boards could not be set, so it is now in state "+
					"with the wrong home boards. Re-apply to retry. "+err.Error(),
			)
			// Fall through: the user exists, so it must be written to state.
		}
	}

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

	if !plan.HomeBoardID.Equal(state.HomeBoardID) || !plan.MobileHomeBoardID.Equal(state.MobileHomeBoardID) {
		if err := r.client.SetUserHomeBoards(ctx, id, stringPointerOrNil(plan.HomeBoardID), stringPointerOrNil(plan.MobileHomeBoardID)); err != nil {
			resp.Diagnostics.AddError("Unable to set the Homarr user's home boards", err.Error())
			return
		}
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
