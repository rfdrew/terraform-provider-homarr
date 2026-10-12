package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ list.ListResource              = &inviteListResource{}
	_ list.ListResourceWithConfigure = &inviteListResource{}
)

// NewInviteListResource returns the homarr_invite list resource.
func NewInviteListResource() list.ListResource { return &inviteListResource{} }

type inviteListResource struct {
	client *client.Client
}

func (l *inviteListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invite"
}

func (l *inviteListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists every Homarr invite, for `terraform query` (Terraform 1.14 and later), in " +
			"Homarr's order: soonest expiration first. Each result carries the invite's identity, ready for an " +
			"`import` block, and is named after its creator and expiration, marked expired once that has " +
			"passed by the clock of the machine running Terraform. With `include_resource = true` it also " +
			"carries the attributes of `homarr_invite`.\n\n" +
			"Expired invites are listed too: Homarr never deletes them. No token is ever listed — Homarr " +
			"returns it only when an invite is created — so an imported invite has none either.\n\n" +
			"Requires an admin API key and the `credentials` auth provider. The list takes no arguments.",
	}
}

func (l *inviteListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	l.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// List reports the invites from one GET /api/invites, which already holds
// every readable attribute, so including the resource costs no further request.
func (l *inviteListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	if l.client == nil {
		stream.Results = unconfiguredListError()
		return
	}

	invites, err := l.client.ListInvites(ctx)
	if err != nil {
		stream.Results = listError("Unable to list Homarr invites", err.Error())
		return
	}

	now := time.Now()
	stream.Results = func(push func(list.ListResult) bool) {
		for i := range invites {
			if listLimitReached(req, i) {
				return
			}
			result := req.NewListResult(ctx)
			result.DisplayName = inviteDisplayName(inviteSummaryFromAPI(&invites[i], now))
			setIdentity(ctx, result.Identity, "id", invites[i].ID, &result.Diagnostics)
			if req.IncludeResource {
				// The same values a refresh after importing the invite records:
				// an expiration in RFC 3339 UTC, and no token.
				invite := inviteModel{Token: types.StringNull()}
				invite.fromAPI(&invites[i])
				result.Diagnostics.Append(result.Resource.Set(ctx, &invite)...)
			}
			if !push(result) {
				return
			}
		}
	}
}

// inviteDisplayName names a listed invite after its creator and expiration,
// for instance "Invite by admin, expires 2027-01-01T00:00:00Z".
func inviteDisplayName(invite inviteSummaryModel) string {
	creator := invite.CreatorName.ValueString()
	if creator == "" {
		creator = invite.CreatorID.ValueString()
	}
	expiry := "expires "
	if invite.Expired.ValueBool() {
		expiry = "expired "
	}
	return "Invite by " + creator + ", " + expiry + invite.ExpirationDate.ValueString()
}
