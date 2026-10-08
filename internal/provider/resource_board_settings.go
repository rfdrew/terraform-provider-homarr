package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// hexColorPattern mirrors hexColorSchema in Homarr: a six-digit hex colour.
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var (
	_ resource.Resource                = &boardSettingsResource{}
	_ resource.ResourceWithConfigure   = &boardSettingsResource{}
	_ resource.ResourceWithImportState = &boardSettingsResource{}
)

// NewBoardSettingsResource returns the homarr_board_settings resource.
func NewBoardSettingsResource() resource.Resource { return &boardSettingsResource{} }

type boardSettingsResource struct {
	client *client.Client
}

type boardSettingsModel struct {
	ID                        types.String `tfsdk:"id"`
	BoardID                   types.String `tfsdk:"board_id"`
	PageTitle                 types.String `tfsdk:"page_title"`
	MetaTitle                 types.String `tfsdk:"meta_title"`
	LogoImageURL              types.String `tfsdk:"logo_image_url"`
	FaviconImageURL           types.String `tfsdk:"favicon_image_url"`
	BackgroundImageURL        types.String `tfsdk:"background_image_url"`
	BackgroundImageAttachment types.String `tfsdk:"background_image_attachment"`
	BackgroundImageRepeat     types.String `tfsdk:"background_image_repeat"`
	BackgroundImageSize       types.String `tfsdk:"background_image_size"`
	PrimaryColor              types.String `tfsdk:"primary_color"`
	SecondaryColor            types.String `tfsdk:"secondary_color"`
	Opacity                   types.Int64  `tfsdk:"opacity"`
	IconColor                 types.String `tfsdk:"icon_color"`
	ItemRadius                types.String `tfsdk:"item_radius"`
	CustomCSS                 types.String `tfsdk:"custom_css"`
	DisableStatus             types.Bool   `tfsdk:"disable_status"`
}

func (r *boardSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board_settings"
}

func (r *boardSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the appearance and behaviour settings of a Homarr board.\n\n" +
			"Every attribute is optional and computed: declare the ones you want to control and leave the " +
			"rest alone. Undeclared attributes are refreshed from Homarr and never reverted, so this " +
			"resource can coexist with settings managed in the UI or elsewhere.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer for drift detection.** `GET /api/boards/{id}/settings` " +
			"arrived in 2.3.0. Against an older Homarr the provider cannot refresh these values and falls " +
			"back to the previous behaviour — state reflects what Terraform last applied, and changes made " +
			"in the UI are neither detected nor reverted. A warning is emitted when that happens.\n\n" +
			"~> **Note** Reading settings requires *modify* access to the board, which is stricter than the " +
			"*view* access needed to list it. Homarr deliberately answers `404` rather than `403` for a " +
			"board you cannot reach, so a key that loses modify access looks the same as a deleted board; " +
			"the provider re-checks the board itself before concluding it is gone.\n\n" +
			"Destroying this resource does not reset anything — board settings are columns on the board " +
			"itself and disappear with it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Same value as `board_id`, present because Terraform requires an `id`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"board_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the board these settings belong to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"page_title": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Title shown in the browser tab for this board.",
			},
			"meta_title": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Value of the page's `<meta>` title, used by link previews.",
			},
			"logo_image_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "URL of the logo shown in the board's header.",
			},
			"favicon_image_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "URL of the board's favicon.",
			},
			"background_image_url": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "URL of the board's background image.",
			},
			"background_image_attachment": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "How the background image scrolls. One of `fixed` or `scroll`.",
				Validators: []validator.String{
					stringvalidator.OneOf("fixed", "scroll"),
				},
			},
			"background_image_repeat": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "How the background image repeats. One of `repeat`, `repeat-x`, " +
					"`repeat-y` or `no-repeat`.",
				Validators: []validator.String{
					stringvalidator.OneOf("repeat", "repeat-x", "repeat-y", "no-repeat"),
				},
			},
			"background_image_size": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "How the background image is scaled. One of `cover` or `contain`.",
				Validators: []validator.String{
					stringvalidator.OneOf("cover", "contain"),
				},
			},
			"primary_color": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Primary accent colour as a six-digit hex value, e.g. `#fa5252`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(hexColorPattern, "must be a six-digit hex colour such as #fa5252"),
				},
			},
			"secondary_color": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Secondary accent colour as a six-digit hex value, e.g. `#fd7e14`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(hexColorPattern, "must be a six-digit hex colour such as #fd7e14"),
				},
			},
			"opacity": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Opacity of board items, a whole number from `0` to `100`.\n\n" +
					"~> **Note** This was a floating-point attribute before. Homarr's schema calls it a " +
					"number but stores an integer column, so a fractional value was silently truncated and " +
					"came back different. It is an integer here to match what Homarr actually keeps.",
				Validators: []validator.Int64{
					int64validator.Between(0, 100),
				},
			},
			"icon_color": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Icon tint as a six-digit hex value. Set to `\"\"` to let Homarr clear it " +
					"back to the default.",
				Validators: []validator.String{
					stringvalidator.Any(
						stringvalidator.RegexMatches(hexColorPattern, "must be a six-digit hex colour such as #ffffff"),
						stringvalidator.LengthAtMost(0),
					),
				},
			},
			"item_radius": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Corner radius of board items. One of `xs`, `sm`, `md`, `lg` or `xl`.",
				Validators: []validator.String{
					stringvalidator.OneOf("xs", "sm", "md", "lg", "xl"),
				},
			},
			"custom_css": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Custom CSS applied to the board. At most 16384 characters.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(16384),
				},
			},
			"disable_status": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Disable the online-status pings for items on this board.",
			},
		},
	}
}

func (r *boardSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI copies a settings read into the model.
//
// Homarr reports the five URL/title fields and iconColor as null when unset,
// and customCss as "" because that column is not nullable. Both map straight
// through: with every attribute Optional+Computed, an undeclared one simply
// adopts whatever Homarr holds.
func (m *boardSettingsModel) fromAPI(settings *client.BoardSettingsRead) {
	m.ID = types.StringValue(settings.ID)
	m.BoardID = types.StringValue(settings.ID)
	m.PageTitle = stringValueOrNull(settings.PageTitle)
	m.MetaTitle = stringValueOrNull(settings.MetaTitle)
	m.LogoImageURL = stringValueOrNull(settings.LogoImageURL)
	m.FaviconImageURL = stringValueOrNull(settings.FaviconImageURL)
	m.BackgroundImageURL = stringValueOrNull(settings.BackgroundImageURL)
	m.BackgroundImageAttachment = types.StringValue(settings.BackgroundImageAttachment)
	m.BackgroundImageRepeat = types.StringValue(settings.BackgroundImageRepeat)
	m.BackgroundImageSize = types.StringValue(settings.BackgroundImageSize)
	m.PrimaryColor = types.StringValue(settings.PrimaryColor)
	m.SecondaryColor = types.StringValue(settings.SecondaryColor)
	m.Opacity = types.Int64Value(int64(settings.Opacity))
	m.IconColor = stringValueOrNull(settings.IconColor)
	m.ItemRadius = types.StringValue(settings.ItemRadius)
	m.CustomCSS = types.StringValue(settings.CustomCSS)
	m.DisableStatus = types.BoolValue(settings.DisableStatus)
}

// toPatch builds the partial settings body. Unset attributes stay nil and are
// omitted, so Homarr leaves them as they are.
func (m *boardSettingsModel) toPatch() client.BoardSettings {
	return client.BoardSettings{
		PageTitle:                 stringPointerOrNil(m.PageTitle),
		MetaTitle:                 stringPointerOrNil(m.MetaTitle),
		LogoImageURL:              stringPointerOrNil(m.LogoImageURL),
		FaviconImageURL:           stringPointerOrNil(m.FaviconImageURL),
		BackgroundImageURL:        stringPointerOrNil(m.BackgroundImageURL),
		BackgroundImageAttachment: stringPointerOrNil(m.BackgroundImageAttachment),
		BackgroundImageRepeat:     stringPointerOrNil(m.BackgroundImageRepeat),
		BackgroundImageSize:       stringPointerOrNil(m.BackgroundImageSize),
		PrimaryColor:              stringPointerOrNil(m.PrimaryColor),
		SecondaryColor:            stringPointerOrNil(m.SecondaryColor),
		Opacity:                   int64PointerOrNil(m.Opacity),
		IconColor:                 stringPointerOrNil(m.IconColor),
		ItemRadius:                stringPointerOrNil(m.ItemRadius),
		CustomCSS:                 stringPointerOrNil(m.CustomCSS),
		DisableStatus:             boolPointerOrNil(m.DisableStatus),
	}
}

// apply writes the patch and then reads the settings back, so that every
// Optional+Computed attribute the configuration left out is filled with what
// Homarr actually holds.
//
// Against a Homarr older than 2.3.0 the read-back is unavailable; the write
// still succeeds and the model keeps the configured values, which is the
// behaviour this resource had before the endpoint existed.
func (r *boardSettingsResource) apply(ctx context.Context, plan *boardSettingsModel, diags *diag.Diagnostics) error {
	boardID := plan.BoardID.ValueString()
	if err := r.client.UpdateBoardSettings(ctx, boardID, plan.toPatch()); err != nil {
		return err
	}
	plan.ID = types.StringValue(boardID)

	settings, err := r.client.GetBoardSettings(ctx, boardID)
	if err != nil {
		if client.IsNotFound(err) {
			diags.AddWarning(
				"Homarr board settings could not be read back",
				"The settings were applied, but GET /api/boards/"+boardID+"/settings is unavailable — "+
					"either this Homarr predates 2.3.0, or the API key lacks modify access to the board. "+
					"State records what was applied, so drift will not be detected.",
			)
			return nil
		}
		return err
	}
	plan.fromAPI(settings)
	return nil
}

func (r *boardSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan boardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan, &resp.Diagnostics); err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddAttributeError(
				path.Root("board_id"),
				"Board not found",
				"Homarr has no board with id "+plan.BoardID.ValueString()+": "+err.Error(),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to apply Homarr board settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the settings from Homarr.
//
// A 404 here is ambiguous: Homarr answers it for a board that does not exist,
// for one the key cannot modify, and — on releases before 2.3.0 — for the
// endpoint itself. The board is therefore re-checked before concluding it is
// gone, so a permission change or an older server degrades to the previous
// write-only behaviour instead of silently dropping the resource from state.
func (r *boardSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state boardSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	boardID := state.BoardID.ValueString()
	settings, err := r.client.GetBoardSettings(ctx, boardID)
	if err == nil {
		state.fromAPI(settings)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	if !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to read Homarr board settings", err.Error())
		return
	}

	if _, boardErr := r.client.GetBoard(ctx, boardID); boardErr != nil {
		if client.IsNotFound(boardErr) {
			// The board is gone, so its settings are gone with it.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to verify the Homarr board still exists", boardErr.Error())
		return
	}

	resp.Diagnostics.AddWarning(
		"Homarr board settings could not be refreshed",
		"The board exists, but GET /api/boards/"+boardID+"/settings answered 404 — either this Homarr "+
			"predates 2.3.0, or the API key lacks modify access to the board. State was left as last "+
			"applied, so drift is not detected.",
	)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *boardSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan boardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan, &resp.Diagnostics); err != nil {
		resp.Diagnostics.AddError("Unable to apply Homarr board settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the resource from state without touching the server.
//
// Board settings are columns on the board row rather than an object of their
// own: there is nothing to delete, and Homarr's endpoint can only set values,
// never reset them to defaults. Deleting the board removes them.
func (r *boardSettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Homarr board settings were left unchanged",
		"homarr_board_settings has been removed from Terraform state, but the settings still apply to the "+
			"board. Homarr's API can only set board settings, never reset them to their defaults. Adjust them "+
			"in the Homarr UI, or delete the board itself, if you need the previous appearance back.",
	)
}

// ImportState imports by board id; Read then populates every attribute.
func (r *boardSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("board_id"), req.ID)...)
}
