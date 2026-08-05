package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ resource.Resource                = &appResource{}
	_ resource.ResourceWithConfigure   = &appResource{}
	_ resource.ResourceWithImportState = &appResource{}
)

// NewAppResource returns the homarr_app resource.
func NewAppResource() resource.Resource { return &appResource{} }

type appResource struct {
	client *client.Client
}

type appModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	IconURL     types.String `tfsdk:"icon_url"`
	Href        types.String `tfsdk:"href"`
	PingURL     types.String `tfsdk:"ping_url"`
}

func (r *appResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (r *appResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr app — a bookmark or shortcut to a service, which boards then " +
			"reference from their app tiles.\n\n" +
			"This is the one Homarr entity with a complete REST lifecycle, so it round-trips cleanly: every " +
			"attribute below is read back on refresh and drift is detected.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr-generated identifier of the app.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the app. Between 1 and 64 characters.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 64),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Optional description shown in the Homarr UI. At most 512 characters. " +
					"An empty string is stored as `null` by Homarr, so use `null` rather than `\"\"` to keep " +
					"plans clean.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(512),
				},
			},
			"icon_url": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "URL of the app's icon. Homarr ships a large icon repository; values such " +
					"as `https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/grafana.svg` are typical.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"href": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "URL the app tile links to. Must be an absolute URL with a scheme; " +
					"`javascript:` is rejected by Homarr.",
			},
			"ping_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "URL Homarr pings to show the app's online status. Must be `http://` or " +
					"`https://`.",
			},
		},
	}
}

func (r *appResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// toRequest builds the create/update payload. Homarr requires every field to be
// present — nullable but not optional — so unset attributes are sent as
// explicit nulls rather than omitted.
func (m *appModel) toRequest() client.AppRequest {
	return client.AppRequest{
		Name:        m.Name.ValueString(),
		Description: stringPointerOrNil(m.Description),
		IconURL:     m.IconURL.ValueString(),
		Href:        stringPointerOrNil(m.Href),
		PingURL:     stringPointerOrNil(m.PingURL),
	}
}

func (m *appModel) fromAPI(app *client.App) {
	m.ID = types.StringValue(app.ID)
	m.Name = types.StringValue(app.Name)
	m.Description = stringValueOrNull(app.Description)
	m.IconURL = types.StringValue(app.IconURL)
	m.Href = stringValueOrNull(app.Href)
	m.PingURL = stringValueOrNull(app.PingURL)
}

func (r *appResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateApp(ctx, plan.toRequest())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Homarr app", err.Error())
		return
	}

	plan.fromAPI(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	app, err := r.client.GetApp(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr app", err.Error())
		return
	}

	state.fromAPI(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *appResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if err := r.client.UpdateApp(ctx, id, plan.toRequest()); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"Homarr app no longer exists",
				"The app "+id+" was deleted outside of Terraform. Run `terraform apply -refresh-only` to "+
					"reconcile state, then apply again.",
			)
			return
		}
		resp.Diagnostics.AddError("Unable to update Homarr app", err.Error())
		return
	}

	// PATCH /api/apps/{id} returns no body, so re-read to pick up Homarr's
	// normalisation — notably empty strings collapsing to null.
	app, err := r.client.GetApp(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr app back after update", err.Error())
		return
	}

	plan.fromAPI(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *appResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteApp(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete Homarr app", err.Error())
	}
}

func (r *appResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
