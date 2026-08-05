package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &appDataSource{}
	_ datasource.DataSourceWithConfigure = &appDataSource{}
)

// NewAppDataSource returns the homarr_app data source.
func NewAppDataSource() datasource.DataSource { return &appDataSource{} }

type appDataSource struct {
	client *client.Client
}

func (d *appDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (d *appDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single Homarr app by `id` or by `name`. Exactly one of the two must be set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Identifier of the app to look up.",
			},
			"name": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Name of the app to look up. Homarr does not enforce unique app names; if " +
					"several apps share a name the lookup fails rather than picking one arbitrarily.",
			},
			"description": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Description of the app.",
			},
			"icon_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Icon URL of the app.",
			},
			"href": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL the app tile links to.",
			},
			"ping_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL Homarr pings for the app's online status.",
			},
		},
	}
}

func (d *appDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *appDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config appModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !config.ID.IsNull() && config.ID.ValueString() != ""
	hasName := !config.Name.IsNull() && config.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError(
			"Ambiguous app lookup",
			"Set either `id` or `name`, not both.",
		)
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError(
			"Missing app lookup key",
			"Set either `id` or `name` to identify the app.",
		)
		return
	}

	var app *client.App
	if hasID {
		found, err := d.client.GetApp(ctx, config.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Unable to read Homarr app", err.Error())
			return
		}
		app = found
	} else {
		apps, err := d.client.ListApps(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Unable to list Homarr apps", err.Error())
			return
		}
		target := config.Name.ValueString()
		var matches []client.App
		for i := range apps {
			if apps[i].Name == target {
				matches = append(matches, apps[i])
			}
		}
		switch len(matches) {
		case 0:
			resp.Diagnostics.AddError(
				"No matching Homarr app",
				"No app is named "+target+".",
			)
			return
		case 1:
			app = &matches[0]
		default:
			resp.Diagnostics.AddError(
				"Multiple matching Homarr apps",
				"Several apps are named "+target+". Look the app up by `id` instead.",
			)
			return
		}
	}

	config.fromAPI(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
