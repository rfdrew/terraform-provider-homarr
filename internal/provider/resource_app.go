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
	_ resource.ResourceWithIdentity    = &appResource{}
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

// Homarr's appManageSchema trims name, description, icon_url, href and
// ping_url, and stores "" as null for the three optional ones. State is written
// from what Homarr answers, so a value it would rewrite is refused at plan time:
// otherwise the apply fails with "Provider produced inconsistent result" and the
// next refresh shows a diff.
func (r *appResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Homarr app — a bookmark or shortcut to a service, which boards then " +
			"reference from their app tiles.\n\n" +
			"This is the one Homarr entity with a complete REST lifecycle, so it round-trips cleanly: every " +
			"attribute below is read back on refresh and drift is detected.\n\n" +
			"~> **Destroying an app reaches beyond the app.** Homarr unlinks every integration associated with " +
			"it — the integration's app becomes none, and only the Homarr UI can link it again — and app tiles " +
			"on boards that show the app stay in place with a reference to an app that no longer exists. The " +
			"same happens when a change forces the app to be replaced. Deleting an app that is already gone " +
			"succeeds, so destroying one removed outside Terraform is not an error.\n\n" +
			"**Permissions.** Creating an app needs the `app-create` permission, updating one `app-modify-all` " +
			"and destroying one `app-full-all`; each includes the ones before it, and admins have all three. " +
			"Refreshing and importing need none of them, because Homarr lets every signed-in user read every " +
			"app. A key whose user can only create apps therefore fails on the first change or destroy.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Homarr-generated identifier of the app.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Display name of the app. Between 1 and 64 characters, with no leading or " +
					"trailing whitespace, which Homarr would strip.",
				Validators: []validator.String{
					jsLengthBetweenValidator(1, 64),
					trimmedValidator(),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Optional description shown in the Homarr UI. At most 512 characters, with " +
					"no leading or trailing whitespace. Leave it out, or set it to `null`, for no description; " +
					"`\"\"` is rejected because Homarr stores it as `null`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					jsLengthAtMostValidator(512),
					trimmedValidator(),
				},
			},
			"icon_url": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "URL of the app's icon, with no leading or trailing whitespace. Homarr ships " +
					"a large icon repository; values such as " +
					"`https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/grafana.svg` are typical.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					trimmedValidator(),
				},
			},
			"href": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "URL or URI the app tile links to, with no leading or trailing whitespace. " +
					"Homarr 1.74.0 and later reject only `javascript:` URIs, in any letter case, and accept " +
					"everything else, including schemes written without `//` such as `mailto:`, and relative " +
					"paths. Homarr 1.73.x additionally required an absolute `scheme://` URL. Leave it out, or set " +
					"it to `null`, for a tile that links nowhere; `\"\"` is rejected because Homarr stores it as " +
					"`null`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					trimmedValidator(),
					appHrefValidator(),
				},
			},
			"ping_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "URL Homarr pings to show the app's online status. Must be an absolute " +
					"`http://` or `https://` URL with a host — the scheme in lower case, as Homarr compares it " +
					"case-sensitively — and no leading or trailing whitespace. Leave it out, or set it to `null`, " +
					"to have Homarr ping `href` instead; `\"\"` is rejected because Homarr stores it as `null`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					trimmedValidator(),
					appPingURLValidator(),
				},
			},
		},
	}
}

func (r *appResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = idIdentitySchema("id", "Homarr-generated identifier of the app.")
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
	setIdentity(ctx, resp.Identity, "id", created.ID, &resp.Diagnostics)
}

func (r *appResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Before the lookup, so that a refresh finding the app gone still reports
	// one; see setIdentity.
	setIdentity(ctx, resp.Identity, "id", state.ID.ValueString(), &resp.Diagnostics)

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

	// PATCH /api/apps/{id} returns no body, so read the app back to record what
	// Homarr stored. The schema refuses the values Homarr would rewrite on the
	// way in — surrounding whitespace, and "" where it stores null — so this
	// matches the plan instead of failing the apply as an inconsistent result.
	app, err := r.client.GetApp(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr app back after update", err.Error())
		return
	}

	plan.fromAPI(app)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
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

// ImportState imports an app by id, given either as the import id or, from
// Terraform 1.12, as the identity of an import block.
func (r *appResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, _ := importedID(ctx, req, "id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	setIdentity(ctx, resp.Identity, "id", id, &resp.Diagnostics)
}
