package provider

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// inviteExpirationPattern is the part of RFC 3339 an invite expiration may use.
//
// Homarr never sees the value as written: Create parses it and the client
// sends the instant again in UTC, so the spelling only has to be one that
// time.Parse reads the same way here and in fromAPI. That already rules out a
// lower-case T or Z. When its RFC 3339 fast path fails, though, time.Parse
// falls back to a looser parser that also takes a comma before the fraction,
// a one-digit hour and offsets such as +24:00 or +23:60. None of those is
// RFC 3339, which is what this attribute documents, so the pattern refuses
// them instead of leaving the accepted spellings to that fallback.
//
// A fraction is allowed only if it is all zeros. Homarr's default SQLite
// database keeps the expiration in whole seconds, so "00:00:00.5Z" comes back
// as "00:00:00Z" — a different instant from the one planned, which Terraform
// rejects as an inconsistent result. PostgreSQL keeps milliseconds, but whole
// seconds are what every Homarr database keeps.
//
// A zero fraction such as ".000", the way Homarr itself prints dates, names
// the same instant as no fraction at all. fromAPI compares instants rather than
// spellings, so it keeps such a value as written, exactly as it keeps an
// offset like +02:00.
var inviteExpirationPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.0+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

// inviteExpirationRequirement describes the values inviteExpirationPattern
// lets through, for both the plan-time validator and Create.
const inviteExpirationRequirement = `must be an RFC 3339 timestamp in whole seconds with an upper-case ` +
	`T and Z, such as "2027-01-01T00:00:00Z" or "2027-01-01T02:00:00+02:00" (Homarr stores the date ` +
	`in whole seconds on SQLite)`

// parseInviteExpiration parses an expiration date that Homarr will accept and
// store as the same instant. The pattern checks the shape; time.Parse then
// checks the ranges — February 30th, hour 24 or a leap second — which zod
// refuses as well. Within that shape the loose fallback has nothing extra to
// accept, so time.Parse refuses exactly the out-of-range fields zod refuses.
func parseInviteExpiration(s string) (time.Time, bool) {
	if !inviteExpirationPattern.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// inviteExpirationValidator rejects at plan time an expiration date that Create
// could not parse, that Homarr would refuse, or that Homarr would store as a
// different instant.
func inviteExpirationValidator() validator.String {
	return stringCheck{
		description: inviteExpirationRequirement,
		check: func(s string) bool {
			_, ok := parseInviteExpiration(s)
			return ok
		},
	}
}

var (
	_ resource.Resource                = &inviteResource{}
	_ resource.ResourceWithConfigure   = &inviteResource{}
	_ resource.ResourceWithImportState = &inviteResource{}
	_ resource.ResourceWithIdentity    = &inviteResource{}
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
			"`https://<your-homarr>/auth/invite/<id>?token=<token>`.\n\n" +
			"**Lifecycle.** An invite works once. Registering through it deletes it, so the next refresh drops " +
			"it from state and the apply after that creates a new invite, with a new token and link. Remove the " +
			"resource once its invite has been used, or rotate it on purpose — for instance by taking " +
			"`expiration_date` from a `time_rotating` resource, since changing it replaces the invite.\n\n" +
			"An invite stops working at `expiration_date`, but Homarr never deletes expired invites: they stay " +
			"in Homarr, and in state, until destroyed. Homarr also accepts an expiration date in the past, which " +
			"creates an invite that can never be used.\n\n" +
			"Homarr deletes an invite together with the user that created it — the user behind the API key. " +
			"Deleting that user, for example in the UI, removes every invite it created, and the next apply " +
			"recreates them with new tokens and links. `homarr_user` refuses to destroy or replace the user " +
			"behind the provider's own key on Homarr 2.3.0 and newer, so through Terraform this happens only " +
			"when the invites were created with another key.\n\n" +
			"The `token` exists in state only when Terraform created the invite: Homarr returns it once, in the " +
			"creation response, so an imported invite has none.",
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
					"`2027-01-01T00:00:00Z` or `2027-01-01T02:00:00+02:00`. It must be a whole second, because " +
					"Homarr stores the date in whole seconds on SQLite: a value such as `2027-01-01T00:00:00.5Z` " +
					"is rejected, while a zero fraction such as `.000` is allowed. `T` and `Z` must be upper " +
					"case, and an offset must be written `+hh:mm` or `-hh:mm`.\n\n" +
					"~> **Note** Homarr has no endpoint to extend an invite, so changing this forces a new " +
					"invite — and therefore a new token and link.",
				Validators: []validator.String{
					inviteExpirationValidator(),
				},
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

func (r *inviteResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema("id", "Homarr-generated identifier of the invite.")
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

	// The validator has already refused anything else at plan time; checking
	// again here keeps a value Homarr would store differently from ever being
	// sent, should a value reach Create unvalidated.
	expiration, ok := parseInviteExpiration(plan.ExpirationDate.ValueString())
	if !ok {
		resp.Diagnostics.AddAttributeError(
			path.Root("expiration_date"),
			"Invalid expiration date",
			fmt.Sprintf("The expiration date %s, got: %q", inviteExpirationRequirement, plan.ExpirationDate.ValueString()),
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
		// The invite exists, and its token will never be returned again, so
		// both go into state before the error is reported. Terraform then
		// keeps the invite as tainted instead of losing track of it, and the
		// token can still be read from state.
		plan.CreatorID = types.StringNull()
		plan.CreatorName = types.StringNull()
		resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
		setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
		resp.Diagnostics.AddError(
			"Unable to read Homarr invite back after creation",
			"Homarr created invite "+id+" and its id and token were saved to state, but reading it back "+
				"failed, so it is marked tainted: the next apply replaces it with a new invite and token. "+
				"Untaint it to keep this one instead. "+err.Error(),
		)
		return
	}
	plan.fromAPI(invite)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
}

func (r *inviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state inviteModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before the lookup, so that a refresh finding the invite gone still
	// reports one; see setIdentity.
	setIdentity(ctx, resp.Identity, "id", state.ID.ValueString(), &resp.Diagnostics)

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
	setIdentity(ctx, resp.Identity, "id", plan.ID.ValueString(), &resp.Diagnostics)
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

// ImportState imports an invite by id, given either as the import id or, from
// Terraform 1.12, as the identity of an import block. The token cannot be
// recovered — Homarr omits it from the list response — so it stays null in
// state.
func (r *inviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, _ := importedID(ctx, req, "id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.AddWarning(
		"The imported invite's token is unavailable",
		"Homarr returns an invite token only once, when the invite is created, so `token` will be null for "+
			"this resource. Recreate the invite if you need a usable link.",
	)
}
