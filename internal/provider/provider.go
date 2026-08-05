// Package provider implements the Terraform provider for Homarr.
package provider

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// Ensure the implementation satisfies the framework interfaces.
var _ provider.Provider = &homarrProvider{}

type homarrProvider struct {
	// version is set at build time and reported to Terraform.
	version string
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &homarrProvider{version: version}
	}
}

type providerModel struct {
	URL                types.String `tfsdk:"url"`
	APIKey             types.String `tfsdk:"api_key"`
	InsecureSkipVerify types.Bool   `tfsdk:"insecure_skip_verify"`
	TimeoutSeconds     types.Int64  `tfsdk:"timeout_seconds"`
}

func (p *homarrProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "homarr"
	resp.Version = p.version
}

func (p *homarrProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages resources in a [Homarr](https://homarr.dev) dashboard instance through its " +
			"OpenAPI-compatible REST API.\n\n" +
			"Only the subset of Homarr's API that is exposed over REST can be managed. Integrations, groups, " +
			"widgets and board items are tRPC-only in Homarr and therefore out of scope for this provider.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL of the Homarr instance, e.g. `https://homarr.example.com`. " +
					"A trailing `/api` is accepted and trimmed. May also be set with the `HOMARR_URL` " +
					"environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Homarr API key in `<id>.<token>` form, created under " +
					"*Management > Tools > API*. The key inherits the permissions of the user that created it; " +
					"managing users, invites and server settings requires an admin user. May also be set with " +
					"the `HOMARR_API_KEY` environment variable.",
			},
			"insecure_skip_verify": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Skip TLS certificate verification. Defaults to `false`. May also be set " +
					"with the `HOMARR_INSECURE_SKIP_VERIFY` environment variable.",
			},
			"timeout_seconds": schema.Int64Attribute{
				Optional: true,
				MarkdownDescription: "Per-request HTTP timeout in seconds. Defaults to `30`. May also be set " +
					"with the `HOMARR_TIMEOUT_SECONDS` environment variable.",
			},
		},
	}
}

func (p *homarrProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Explicit configuration wins over the environment.
	url := os.Getenv("HOMARR_URL")
	if !config.URL.IsNull() && !config.URL.IsUnknown() {
		url = config.URL.ValueString()
	}
	apiKey := os.Getenv("HOMARR_API_KEY")
	if !config.APIKey.IsNull() && !config.APIKey.IsUnknown() {
		apiKey = config.APIKey.ValueString()
	}

	insecure := false
	if env := os.Getenv("HOMARR_INSECURE_SKIP_VERIFY"); env != "" {
		parsed, err := strconv.ParseBool(env)
		if err != nil {
			resp.Diagnostics.AddError(
				"Invalid HOMARR_INSECURE_SKIP_VERIFY",
				"Expected a boolean such as \"true\" or \"false\", got "+strconv.Quote(env)+".",
			)
			return
		}
		insecure = parsed
	}
	if !config.InsecureSkipVerify.IsNull() && !config.InsecureSkipVerify.IsUnknown() {
		insecure = config.InsecureSkipVerify.ValueBool()
	}

	timeout := 30 * time.Second
	if env := os.Getenv("HOMARR_TIMEOUT_SECONDS"); env != "" {
		parsed, err := strconv.Atoi(env)
		if err != nil || parsed <= 0 {
			resp.Diagnostics.AddError(
				"Invalid HOMARR_TIMEOUT_SECONDS",
				"Expected a positive integer number of seconds, got "+strconv.Quote(env)+".",
			)
			return
		}
		timeout = time.Duration(parsed) * time.Second
	}
	if !config.TimeoutSeconds.IsNull() && !config.TimeoutSeconds.IsUnknown() {
		if config.TimeoutSeconds.ValueInt64() <= 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root("timeout_seconds"),
				"Invalid timeout",
				"timeout_seconds must be greater than zero.",
			)
			return
		}
		timeout = time.Duration(config.TimeoutSeconds.ValueInt64()) * time.Second
	}

	if url == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("url"),
			"Missing Homarr URL",
			"Set the provider's `url` attribute or the HOMARR_URL environment variable.",
		)
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Homarr API key",
			"Set the provider's `api_key` attribute or the HOMARR_API_KEY environment variable. "+
				"Keys are created under Management > Tools > API and look like `<id>.<token>`.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	homarr, err := client.New(client.Options{
		BaseURL:            url,
		APIKey:             apiKey,
		InsecureSkipVerify: insecure,
		Timeout:            timeout,
		UserAgent:          "terraform-provider-homarr/" + p.version,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid Homarr provider configuration", err.Error())
		return
	}

	// GET /api/info is the cheapest authenticated endpoint, so it doubles as a
	// connectivity and credential check. Failing here produces one clear error
	// instead of the same failure repeated across every resource.
	info, err := homarr.GetInfo(ctx)
	if err != nil {
		detail := "Could not reach the Homarr API at " + homarr.BaseURL() + ": " + err.Error()
		if apiErr, ok := err.(*client.Error); ok && apiErr.StatusCode == 401 {
			detail = "Homarr rejected the API key (HTTP 401). Check that `api_key` is a full `<id>.<token>` " +
				"pair and that the key has not been deleted."
		}
		resp.Diagnostics.AddError("Unable to connect to Homarr", detail)
		return
	}
	tflog.Info(ctx, "connected to Homarr", map[string]any{"url": homarr.BaseURL(), "version": info.Version})

	resp.DataSourceData = homarr
	resp.ResourceData = homarr
}

func (p *homarrProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAppResource,
		NewBoardResource,
		NewBoardSettingsResource,
		NewUserResource,
		NewInviteResource,
		NewServerBoardSettingsResource,
	}
}

func (p *homarrProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAppDataSource,
		NewAppsDataSource,
		NewBoardDataSource,
		NewBoardsDataSource,
		NewUsersDataSource,
		NewInfoDataSource,
	}
}

// clientFromProviderData pulls the configured client out of the framework's
// provider data, guarding against the nil that appears during early validation
// walks.
func clientFromProviderData(providerData any, diags interface {
	AddError(summary, detail string)
}) *client.Client {
	if providerData == nil {
		return nil
	}
	homarr, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			"The provider was configured with an unexpected internal type. This is a bug in the provider.",
		)
		return nil
	}
	return homarr
}
