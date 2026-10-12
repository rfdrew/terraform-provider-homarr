package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// integrationConnectionRelease added GET /api/integrations/{id}/test-connection.
var integrationConnectionRelease = homarrRelease{2, 1, 0}

var (
	_ datasource.DataSource              = &integrationConnectionDataSource{}
	_ datasource.DataSourceWithConfigure = &integrationConnectionDataSource{}
)

// NewIntegrationConnectionDataSource returns the homarr_integration_connection
// data source.
func NewIntegrationConnectionDataSource() datasource.DataSource {
	return &integrationConnectionDataSource{}
}

type integrationConnectionDataSource struct {
	client *client.Client
}

type integrationConnectionDataSourceModel struct {
	IntegrationID types.String `tfsdk:"integration_id"`
	Success       types.Bool   `tfsdk:"success"`
	Error         types.String `tfsdk:"error"`
}

func (d *integrationConnectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_integration_connection"
}

func (d *integrationConnectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Tests the connection of a saved Homarr integration with the credentials Homarr " +
			"stores for it — a health probe meant for `check` blocks and postconditions, not for feeding other " +
			"resources.\n\n" +
			"~> **Every read performs a live connection test.** Homarr contacts the integrated service on each " +
			"plan and refresh, and may log in to it and open a session there. Nothing in Homarr is changed.\n\n" +
			"~> **Requires Homarr 2.1.0 or newer**, which added `GET /api/integrations/{id}/test-connection`.\n\n" +
			"The API key needs *full* access to the integration: the `integration-full-all` permission, which " +
			"admins have, or a *full* permission on that integration. Homarr answers `404` rather than `403` " +
			"without it, so an insufficient key looks like a missing integration.\n\n" +
			"Integrations cannot be listed or managed over Homarr's REST API, so the id has to be copied from " +
			"the integration's edit page in the UI, whose address ends in `/manage/integrations/edit/<id>`.\n\n" +
			"A failed connection is not an error: `success` is `false` and `error` names the reason. Inside a " +
			"`check` block, an error reading the data source itself — a wrong id, say — is reported as a " +
			"warning rather than failing the run.",
		Attributes: map[string]schema.Attribute{
			"integration_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the integration to test.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"success": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Whether Homarr's connection check for the integration passed. It means only " +
					"that the integration's native check succeeded, not that every widget using it works.",
			},
			"error": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Why the check failed, or null when it passed. Homarr reports only a " +
					"category, never the service's own message: `missingSecret` (a required credential is not " +
					"stored), `authorization` (the service refused the credentials), `certificate` (the " +
					"service's TLS certificate is not trusted — see `homarr_root_certificate`), `request` (the " +
					"service could not be reached), `statusCode` (it answered with an unexpected HTTP status), " +
					"`parse` (its answer could not be understood) or `unknown`.",
			},
		},
	}
}

func (d *integrationConnectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *integrationConnectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config integrationConnectionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := config.IntegrationID.ValueString()
	result, err := d.client.TestIntegrationConnection(ctx, id)
	if err != nil {
		if !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Unable to test Homarr integration connection", err.Error())
			return
		}
		// The same 404 comes back for a missing integration, for one the key
		// cannot fully access, and — before 2.1.0 — for the route itself.
		older, known, version := homarrPredates(ctx, d.client, integrationConnectionRelease)
		if older {
			resp.Diagnostics.AddError(
				requiresHomarrSummary(integrationConnectionRelease),
				requiresHomarrDetail("homarr_integration_connection", "GET /api/integrations/{id}/test-connection",
					integrationConnectionRelease, version),
			)
			return
		}
		detail := "No integration with id " + id + " is available to this API key. Homarr answers 404 both " +
			"for an integration that does not exist and for one the key lacks full access to, so check the id " +
			"and the key's permissions."
		if !known {
			detail += " If this Homarr predates 2.1.0, the endpoint itself is missing as well."
		}
		resp.Diagnostics.AddAttributeError(path.Root("integration_id"), "Homarr integration not found", detail)
		return
	}

	config.Success = types.BoolValue(result.Success)
	config.Error = stringValueOrNull(result.Error)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
