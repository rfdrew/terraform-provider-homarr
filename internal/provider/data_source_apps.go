package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &appsDataSource{}
	_ datasource.DataSourceWithConfigure = &appsDataSource{}
)

// NewAppsDataSource returns the homarr_apps data source.
func NewAppsDataSource() datasource.DataSource { return &appsDataSource{} }

type appsDataSource struct {
	client *client.Client
}

type appsDataSourceModel struct {
	Apps []appModel `tfsdk:"apps"`
}

func (d *appsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_apps"
}

func (d *appsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Homarr app visible to the configured API key.",
		Attributes: map[string]schema.Attribute{
			"apps": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "All apps, in the order Homarr returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "Identifier of the app."},
						"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the app."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Description of the app."},
						"icon_url":    schema.StringAttribute{Computed: true, MarkdownDescription: "Icon URL of the app."},
						"href":        schema.StringAttribute{Computed: true, MarkdownDescription: "URL the app tile links to."},
						"ping_url":    schema.StringAttribute{Computed: true, MarkdownDescription: "URL Homarr pings for the app's online status."},
					},
				},
			},
		},
	}
}

func (d *appsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *appsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	apps, err := d.client.ListApps(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Homarr apps", err.Error())
		return
	}

	state := appsDataSourceModel{Apps: make([]appModel, 0, len(apps))}
	for i := range apps {
		var item appModel
		item.fromAPI(&apps[i])
		state.Apps = append(state.Apps, item)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
