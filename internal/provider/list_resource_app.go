package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ list.ListResource              = &appListResource{}
	_ list.ListResourceWithConfigure = &appListResource{}
)

// NewAppListResource returns the homarr_app list resource.
func NewAppListResource() list.ListResource { return &appListResource{} }

type appListResource struct {
	client *client.Client
}

func (l *appListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (l *appListResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists every Homarr app visible to the API key, for `terraform query` (Terraform 1.14 " +
			"and later). Each result is named after the app and carries its identity, ready for an `import` " +
			"block; with `include_resource = true` it also carries every attribute of `homarr_app`.\n\n" +
			"Any API key can list apps: a key without the `app-modify-all` permission is answered from " +
			"`GET /api/apps/selectable`, which reports the same fields. The list takes no arguments.",
	}
}

func (l *appListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	l.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// List reports the apps in the order Homarr returns them. The listing already
// holds every attribute, so including the resource costs no further request.
func (l *appListResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	if l.client == nil {
		stream.Results = unconfiguredListError()
		return
	}

	apps, err := l.client.ListApps(ctx)
	if err != nil {
		stream.Results = listError("Unable to list Homarr apps", err.Error())
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range apps {
			if listLimitReached(req, i) {
				return
			}
			result := req.NewListResult(ctx)
			result.DisplayName = apps[i].Name
			setIdentity(ctx, result.Identity, "id", apps[i].ID, &result.Diagnostics)
			if req.IncludeResource {
				var app appModel
				app.fromAPI(&apps[i])
				result.Diagnostics.Append(result.Resource.Set(ctx, &app)...)
			}
			if !push(result) {
				return
			}
		}
	}
}
