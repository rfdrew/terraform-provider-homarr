package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ resource.Resource                = &inviteResource{}
	_ resource.ResourceWithConfigure   = &inviteResource{}
	_ resource.ResourceWithImportState = &inviteResource{}
)

// NewInviteResource returns the homarr_invite resource.
func NewInviteResource() resource.Resource { return &inviteResource{} }

type inviteResource struct {
	client *client.Client
}

type inviteModel struct {
	ID             types.String `tfsdk:"id"`
	ExpirationDate types.String `tfsdk:"expiration_date"`
	Token          types.String `tfsdk:"token"`
	CreatorID      types.String `tfsdk:"creator_id"`
	CreatorName    types.String `tfsdk:"creator_name"`
}

func (r *inviteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invite"
}

func (r *inviteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr registration invite.\n\n" +
			"Requires an admin API key and the `credentials` auth provider. The invite link a person follows is " +
			"`https://<your-homarr>/auth/invite/<id>?token=<token>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr-generated identifier of the invite.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"expiration_date": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Moment the invite stops working, as an RFC 3339 timestamp such as " +
					"`2027-01-01T00:00:00Z`.\n\n" +
					"~> **Note** Homarr has no endpoint to extend an invite, so changing this forces a new " +
					"invite — and therefore a new token and link.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"token": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "Secret token that completes the invite link. Homarr returns it exactly " +
					"once, at creation, and never again — it is stored in Terraform state and cannot be " +
					"recovered if lost.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"creator_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Identifier of the user that created the invite — the user behind the API key.",
			},
			"creator_name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the user that created the invite.",
			},
		},
	}
}

func (r *inviteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies readable fields into the model. The token is left alone: it is
// absent from every response except the creation one.
func (m *inviteModel) fromAPI(invite *client.Invite) {
	m.ID = types.StringValue(invite.ID)
	m.CreatorID = types.StringValue(invite.Creator.ID)
	m.CreatorName = stringValueOrNull(invite.Creator.Name)

	// Homarr echoes the timestamp it stored. Re-serialising it in the same
	// layout the user wrote keeps the plan empty across refreshes.
	if parsed, err := time.Parse(time.RFC3339, invite.ExpirationDate); err == nil {
		if existing, err := time.Parse(time.RFC3339, m.ExpirationDate.ValueString()); err == nil && existing.Equal(parsed) {
			return
		}
		m.ExpirationDate = types.StringValue(parsed.UTC().Format(time.RFC3339))
		return
	}
	m.ExpirationDate = types.StringValue(invite.ExpirationDate)
}

func (r *inviteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan inviteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	expiration, err := time.Parse(time.RFC3339, plan.ExpirationDate.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("expiration_date"),
			"Invalid expiration date",
			"Expected an RFC 3339 timestamp such as \"2027-01-01T00:00:00Z\": "+err.Error(),
		)
		return
	}

	id, token, err := r.client.CreateInvite(ctx, expiration)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Homarr invite", err.Error())
		return
	}

	plan.ID = types.StringValue(id)
	plan.Token = types.StringValue(token)

	invite, err := r.client.GetInvite(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr invite back after creation", err.Error())
		return
	}
	plan.fromAPI(invite)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *inviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state inviteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	invite, err := r.client.GetInvite(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr invite", err.Error())
		return
	}

	state.fromAPI(invite)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update exists only to satisfy the interface: every writable attribute forces
// replacement, so Terraform never calls it.
func (r *inviteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan inviteModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *inviteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state inviteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteInvite(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete Homarr invite", err.Error())
	}
}

// ImportState imports an invite by id. The token cannot be recovered — Homarr
// omits it from the list response — so it stays null in state.
func (r *inviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.AddWarning(
		"The imported invite's token is unavailable",
		"Homarr returns an invite token only once, when the invite is created, so `token` will be null for "+
			"this resource. Recreate the invite if you need a usable link.",
	)
}
