package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// boardDataSourceModel is the flattened board summary shared by the single and
// plural board data sources. It omits column_count, which Homarr's REST API
// never reports.
type boardDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	LogoImageURL types.String `tfsdk:"logo_image_url"`
	IsPublic     types.Bool   `tfsdk:"is_public"`
	CreatorID    types.String `tfsdk:"creator_id"`
	CreatorName  types.String `tfsdk:"creator_name"`
	IsHome       types.Bool   `tfsdk:"is_home"`
	IsMobileHome types.Bool   `tfsdk:"is_mobile_home"`
}

func (m *boardDataSourceModel) fromAPI(board *client.BoardSummary) {
	m.ID = types.StringValue(board.ID)
	m.Name = types.StringValue(board.Name)
	m.LogoImageURL = stringValueOrNull(board.LogoImageURL)
	m.IsPublic = types.BoolValue(board.IsPublic)
	m.IsHome = types.BoolValue(board.IsHome)
	m.IsMobileHome = types.BoolValue(board.IsMobileHome)
	if board.Creator != nil {
		m.CreatorID = types.StringValue(board.Creator.ID)
		m.CreatorName = stringValueOrNull(board.Creator.Name)
	} else {
		m.CreatorID = types.StringNull()
		m.CreatorName = types.StringNull()
	}
}

func boardDataSourceAttributes(lookupKeys bool) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the board.",
		},
		"name": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Name of the board.",
		},
		"logo_image_url": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Board logo URL.",
		},
		"is_public": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether the board is visible without authentication.",
		},
		"creator_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the board's creator.",
		},
		"creator_name": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Name of the board's creator.",
		},
		"is_home": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether this is the home board of the user behind the API key.",
		},
		"is_mobile_home": schema.BoolAttribute{
			Computed:            true,
			MarkdownDescription: "Whether this is the mobile home board of the user behind the API key.",
		},
	}
	if lookupKeys {
		attrs["id"] = schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Identifier of the board to look up.",
		}
		attrs["name"] = schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Name of the board to look up. Board names are unique in Homarr.",
		}
	}
	return attrs
}

var (
	_ datasource.DataSource              = &boardDataSource{}
	_ datasource.DataSourceWithConfigure = &boardDataSource{}
)

// NewBoardDataSource returns the homarr_board data source.
func NewBoardDataSource() datasource.DataSource { return &boardDataSource{} }

type boardDataSource struct {
	client *client.Client
}

func (d *boardDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_board"
}

func (d *boardDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single Homarr board by `id` or by `name`. Exactly one of the two must " +
			"be set.\n\n" +
			"Note that the board's column count is absent: Homarr's REST API does not expose it.",
		Attributes: boardDataSourceAttributes(true),
	}
}

func (d *boardDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *boardDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config boardDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !config.ID.IsNull() && config.ID.ValueString() != ""
	hasName := !config.Name.IsNull() && config.Name.ValueString() != ""

	switch {
	case hasID && hasName:
		resp.Diagnostics.AddError("Ambiguous board lookup", "Set either `id` or `name`, not both.")
		return
	case !hasID && !hasName:
		resp.Diagnostics.AddError("Missing board lookup key", "Set either `id` or `name` to identify the board.")
		return
	}

	var (
		board *client.BoardSummary
		err   error
	)
	if hasID {
		board, err = d.client.GetBoard(ctx, config.ID.ValueString())
	} else {
		board, err = d.client.GetBoardByName(ctx, config.Name.ValueString())
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Homarr board", err.Error())
		return
	}

	config.fromAPI(board)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

var (
	_ datasource.DataSource              = &boardsDataSource{}
	_ datasource.DataSourceWithConfigure = &boardsDataSource{}
)

// NewBoardsDataSource returns the homarr_boards data source.
func NewBoardsDataSource() datasource.DataSource { return &boardsDataSource{} }

type boardsDataSource struct {
	client *client.Client
}

type boardsDataSourceModel struct {
	Boards []boardDataSourceModel `tfsdk:"boards"`
}

func (d *boardsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_boards"
}

func (d *boardsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Homarr board visible to the configured API key.",
		Attributes: map[string]schema.Attribute{
			"boards": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "All boards, in the order Homarr returns them.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: boardDataSourceAttributes(false),
				},
			},
		},
	}
}

func (d *boardsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

func (d *boardsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	boards, err := d.client.ListBoards(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list Homarr boards", err.Error())
		return
	}

	state := boardsDataSourceModel{Boards: make([]boardDataSourceModel, 0, len(boards))}
	for i := range boards {
		var item boardDataSourceModel
		item.fromAPI(&boards[i])
		state.Boards = append(state.Boards, item)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
