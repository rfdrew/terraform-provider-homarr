package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
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
	ID                        types.String  `tfsdk:"id"`
	BoardID                   types.String  `tfsdk:"board_id"`
	PageTitle                 types.String  `tfsdk:"page_title"`
	MetaTitle                 types.String  `tfsdk:"meta_title"`
	LogoImageURL              types.String  `tfsdk:"logo_image_url"`
	FaviconImageURL           types.String  `tfsdk:"favicon_image_url"`
	BackgroundImageURL        types.String  `tfsdk:"background_image_url"`
	BackgroundImageAttachment types.String  `tfsdk:"background_image_attachment"`
	BackgroundImageRepeat     types.String  `tfsdk:"background_image_repeat"`
	BackgroundImageSize       types.String  `tfsdk:"background_image_size"`
	PrimaryColor              types.String  `tfsdk:"primary_color"`
	SecondaryColor            types.String  `tfsdk:"secondary_color"`
	Opacity                   types.Float64 `tfsdk:"opacity"`
	IconColor                 types.String  `tfsdk:"icon_color"`
	ItemRadius                types.String  `tfsdk:"item_radius"`
	CustomCSS                 types.String  `tfsdk:"custom_css"`
	DisableStatus             types.Bool    `tfsdk:"disable_status"`
}

func (r *boardSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board_settings"
}

func (r *boardSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the appearance and behaviour settings of a Homarr board.\n\n" +
			"~> **This resource is write-only.** Homarr exposes `PATCH /api/boards/{id}/settings` but has no " +
			"matching `GET` — per-board settings are only readable through its tRPC API. The provider therefore " +
			"cannot refresh these values, and state reflects the last configuration Terraform applied rather " +
			"than what the server currently holds. Practical consequences:\n\n" +
			"* Changes made in the Homarr UI are **not** detected as drift and will **not** be reverted until " +
			"  something in this resource's configuration changes.\n" +
			"* Removing an attribute from the configuration sends no reset — Homarr keeps the previously " +
			"  applied value. Set the attribute to the value you want instead of deleting it.\n" +
			"* Destroying this resource is a no-op on the server; it only drops the resource from state.\n\n" +
			"Only attributes you actually set are sent, so this resource can coexist with settings managed " +
			"elsewhere.",
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
				MarkdownDescription: "Title shown in the browser tab for this board.",
			},
			"meta_title": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Value of the page's `<meta>` title, used by link previews.",
			},
			"logo_image_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL of the logo shown in the board's header.",
			},
			"favicon_image_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL of the board's favicon.",
			},
			"background_image_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "URL of the board's background image.",
			},
			"background_image_attachment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "How the background image scrolls. One of `fixed` or `scroll`.",
				Validators: []validator.String{
					stringvalidator.OneOf("fixed", "scroll"),
				},
			},
			"background_image_repeat": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "How the background image repeats. One of `repeat`, `repeat-x`, " +
					"`repeat-y` or `no-repeat`.",
				Validators: []validator.String{
					stringvalidator.OneOf("repeat", "repeat-x", "repeat-y", "no-repeat"),
				},
			},
			"background_image_size": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "How the background image is scaled. One of `cover` or `contain`.",
				Validators: []validator.String{
					stringvalidator.OneOf("cover", "contain"),
				},
			},
			"primary_color": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Primary accent colour as a six-digit hex value, e.g. `#fa5252`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(hexColorPattern, "must be a six-digit hex colour such as #fa5252"),
				},
			},
			"secondary_color": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Secondary accent colour as a six-digit hex value, e.g. `#fd7e14`.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(hexColorPattern, "must be a six-digit hex colour such as #fd7e14"),
				},
			},
			"opacity": schema.Float64Attribute{
				Optional:            true,
				MarkdownDescription: "Opacity of board items, from `0` to `100`.",
				Validators: []validator.Float64{
					float64validator.Between(0, 100),
				},
			},
			"icon_color": schema.StringAttribute{
				Optional: true,
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
				MarkdownDescription: "Corner radius of board items. One of `xs`, `sm`, `md`, `lg` or `xl`.",
				Validators: []validator.String{
					stringvalidator.OneOf("xs", "sm", "md", "lg", "xl"),
				},
			},
			"custom_css": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Custom CSS applied to the board. At most 16384 characters.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(16384),
				},
			},
			"disable_status": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Disable the online-status pings for items on this board.",
			},
		},
	}
}

func (r *boardSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
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
		Opacity:                   float64PointerOrNil(m.Opacity),
		IconColor:                 stringPointerOrNil(m.IconColor),
		ItemRadius:                stringPointerOrNil(m.ItemRadius),
		CustomCSS:                 stringPointerOrNil(m.CustomCSS),
		DisableStatus:             boolPointerOrNil(m.DisableStatus),
	}
}

func (r *boardSettingsResource) apply(ctx context.Context, plan *boardSettingsModel) error {
	boardID := plan.BoardID.ValueString()
	if err := r.client.UpdateBoardSettings(ctx, boardID, plan.toPatch()); err != nil {
		return err
	}
	plan.ID = types.StringValue(boardID)
	return nil
}

func (r *boardSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan boardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
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

// Read verifies only that the board still exists.
//
// The settings themselves cannot be read back — Homarr has no GET for them — so
// state is left exactly as applied. See the resource description for what that
// means for drift detection.
func (r *boardSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state boardSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.client.GetBoard(ctx, state.BoardID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			// The board is gone, so its settings are gone with it.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to verify the Homarr board still exists", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *boardSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan boardSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Unable to apply Homarr board settings", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the resource from state without touching the server.
//
// Homarr's settings endpoint can only set values, not reset them to defaults,
// and the provider does not know what the defaults were before it first applied.
// Reverting would mean guessing, so it does nothing and says so.
func (r *boardSettingsResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Homarr board settings were left unchanged",
		"homarr_board_settings has been removed from Terraform state, but the settings still apply to the "+
			"board. Homarr's API can only set board settings, never reset them to their defaults. Adjust them "+
			"in the Homarr UI if you need the previous appearance back.",
	)
}

// ImportState imports by board id. Because the settings cannot be read back,
// every configured attribute will show as a change on the first plan after
// import; applying it makes state and server agree.
func (r *boardSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("board_id"), req.ID)...)
	resp.Diagnostics.AddWarning(
		"Imported Homarr board settings are not readable",
		"Homarr has no endpoint to read per-board settings, so nothing could be populated beyond the board id. "+
			"The next plan will propose applying every attribute in your configuration.",
	)
}
