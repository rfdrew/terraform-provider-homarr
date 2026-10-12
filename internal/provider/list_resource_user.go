package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ list.ListResource              = &userListResource{}
	_ list.ListResourceWithConfigure = &userListResource{}
)

// NewUserListResource returns the homarr_user list resource.
func NewUserListResource() list.ListResource { return &userListResource{} }

type userListResource struct {
	client *client.Client
}

func (l *userListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (l *userListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the Homarr users that `homarr_user` can manage — those that sign in with " +
			"credentials — for `terraform query` (Terraform 1.14 and later), sorted by username and then id. " +
			"Accounts backed by LDAP or OIDC are left out, because `homarr_user` refuses to import them; " +
			"`homarr_user_preferences` manages those, and the `homarr_users` data source lists them. Each " +
			"result is named after the username and carries the user's identity, ready for an `import` " +
			"block.\n\n" +
			"Listing works with any API key. With `include_resource = true` every user is also read the way " +
			"Terraform reads one after importing it, which takes two requests per user and an admin key, as " +
			"`homarr_user` itself does. The password is never readable, so it is always null. The list takes " +
			"no arguments.",
	}
}

func (l *userListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	l.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// List reports the credentials users from GET /api/users/selectable, which any
// key may call and which filters by auth provider on the server. Including the
// resource needs more than that list holds — every preference and the header —
// so each user is then read by homarr_user's own Read.
func (l *userListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	if l.client == nil {
		stream.Results = unconfiguredListError()
		return
	}

	users, err := l.client.ListSelectableUsers(ctx, "credentials")
	if err != nil {
		stream.Results = listError("Unable to list Homarr users", err.Error())
		return
	}
	slices.SortFunc(users, compareUsers)

	stream.Results = func(push func(list.ListResult) bool) {
		produced := 0
		for i := range users {
			if listLimitReached(req, produced) {
				return
			}
			result := req.NewListResult(ctx)
			result.DisplayName = users[i].ID
			if users[i].Name != nil {
				result.DisplayName = *users[i].Name
			}
			setIdentity(ctx, result.Identity, "id", users[i].ID, &result.Diagnostics)
			if req.IncludeResource && !l.readListedUser(ctx, users[i].ID, &result) {
				// Deleted since it was listed.
				continue
			}
			produced++
			if !push(result) {
				return
			}
		}
	}
}

// readListedUser fills result.Resource the way Terraform reads a user right
// after importing it: homarr_user's Read, from a state that holds only the id.
// It reports false for a user that turned out to be gone.
func (l *userListResource) readListedUser(ctx context.Context, id string, result *list.ListResult) bool {
	state := tfsdk.State{Schema: result.Resource.Schema, Raw: result.Resource.Raw.Copy()}
	result.Diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), id)...)
	if result.Diagnostics.HasError() {
		return true
	}

	readReq := resource.ReadRequest{
		State:    state,
		Identity: &tfsdk.ResourceIdentity{Schema: result.Identity.Schema, Raw: result.Identity.Raw.Copy()},
	}
	readResp := resource.ReadResponse{
		State:    tfsdk.State{Schema: state.Schema, Raw: state.Raw.Copy()},
		Identity: result.Identity,
	}
	(&userResource{client: l.client}).Read(ctx, readReq, &readResp)
	result.Diagnostics.Append(readResp.Diagnostics...)
	if result.Diagnostics.HasError() {
		return true
	}
	if readResp.State.Raw.IsNull() {
		return false
	}
	result.Resource.Raw = readResp.State.Raw
	return true
}
