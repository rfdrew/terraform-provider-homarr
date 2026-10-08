package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

var (
	_ datasource.DataSource              = &boardSettingsDataSource{}
	_ datasource.DataSourceWithConfigure = &boardSettingsDataSource{}
)

// NewBoardSettingsDataSource returns the homarr_board_settings data source.
func NewBoardSettingsDataSource() datasource.DataSource { return &boardSettingsDataSource{} }

type boardSettingsDataSource struct {
	client *client.Client
}

func (d *boardSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board_settings"
}

func (d *boardSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the appearance and behaviour settings of a Homarr board.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer**, which added `GET /api/boards/{id}/settings`.\n\n" +
			"~> **Needs *modify* access to the board**, not the *view* access that listing boards requires — " +
			"unusual for a data source, and a read-only API key will not work. Homarr answers `404` rather " +
			"than `403` for a board it will not show you, so an insufficient key looks like a missing board.\n\n" +
			"These fields are deliberately absent from `homarr_board` and its data sources, which are built " +
			"on `GET /api/boards` and need only *view*; folding them in there would raise that permission " +
			"floor for every existing configuration.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the board to read.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Name of the board, as reported alongside its settings.",
			},
			"page_title": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Title shown in the browser tab for this board.",
			},
			"meta_title": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Value of the page's `<meta>` title, used by link previews.",
			},
			"logo_image_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL of the logo shown in the board's header.",
			},
			"favicon_image_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL of the board's favicon.",
			},
			"background_image_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "URL of the board's background image.",
			},
			"background_image_attachment": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the background image scrolls, `fixed` or `scroll`.",
			},
			"background_image_repeat": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the background image repeats.",
			},
			"background_image_size": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "How the background image is scaled, `cover` or `contain`.",
			},
			"primary_color": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Primary accent colour as a six-digit hex value.",
			},
			"secondary_color": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Secondary accent colour as a six-digit hex value.",
			},
			"opacity": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Opacity of board items, from `0` to `100`.",
			},
			"icon_color": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Icon tint as a six-digit hex value, or null when unset.",
			},
			"item_radius": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Corner radius of board items.",
			},
			"custom_css": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Custom CSS applied to the board. Homarr reports `\"\"` rather than " +
					"null when no CSS is set, because that column is not nullable.",
			},
			"disable_status": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether online-status pings are disabled for items on this board.",
			},
		},
	}
}

func (d *boardSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

type boardSettingsDataSourceModel struct {
	ID                        types.String `tfsdk:"id"`
	Name                      types.String `tfsdk:"name"`
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

func (m *boardSettingsDataSourceModel) fromAPI(settings *client.BoardSettingsRead) {
	m.ID = types.StringValue(settings.ID)
	m.Name = types.StringValue(settings.Name)
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

func (d *boardSettingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config boardSettingsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	settings, err := d.client.GetBoardSettings(ctx, config.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"Homarr board settings not found",
				"No board with id "+config.ID.ValueString()+" is readable with this API key. Homarr "+
					"answers 404 both for a board that does not exist and for one the key cannot modify, "+
					"so check the id and the key's permissions.",
			)
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr board settings", err.Error())
		return
	}

	config.fromAPI(settings)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
