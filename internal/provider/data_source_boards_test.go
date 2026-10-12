package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func readDataSource(t *testing.T, d datasource.DataSource, config any) datasource.ReadResponse {
	t.Helper()
	ctx := context.Background()
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema

	raw := tftypes.NewValue(s.Type().TerraformType(ctx), nil)
	cfg := tfsdk.State{Schema: s, Raw: raw}
	if config != nil {
		if diags := cfg.Set(ctx, config); diags.HasError() {
			t.Fatalf("building config: %v", diags)
		}
	}
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: raw}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: cfg.Raw}}, &resp)
	return resp
}

const boardsOutOfOrder = `[` +
	`{"id":"b3","name":"media","isPublic":true,"isHome":false,"isMobileHome":false},` +
	`{"id":"b2","name":"Infra","isPublic":true,"isHome":false,"isMobileHome":false},` +
	`{"id":"b1","name":"infra","isPublic":true,"isHome":false,"isMobileHome":false},` +
	`{"id":"b4","name":"Apps","isPublic":true,"isHome":false,"isMobileHome":false}]`

func TestBoardsDataSourceSortsByName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsOutOfOrder)

	resp := readDataSource(t, &boardsDataSource{client: fake.client(t)}, nil)
	requireNoErrors(t, resp.Diagnostics)

	var got boardsDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	var ids []string
	for _, b := range got.Boards {
		ids = append(ids, b.ID.ValueString())
	}
	want := []string{"b4", "b2", "b1", "b3"} // Apps, Infra, infra, media
	if len(ids) != len(want) {
		t.Fatalf("boards = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("boards = %v, want %v", ids, want)
		}
	}
}

func boardLookup(name string) *boardDataSourceModel {
	return &boardDataSourceModel{Name: types.StringValue(name), ID: types.StringNull()}
}

func TestBoardDataSourceMatchesANameCaseInsensitively(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsOutOfOrder)

	resp := readDataSource(t, &boardDataSource{client: fake.client(t)}, boardLookup("MEDIA"))
	requireNoErrors(t, resp.Diagnostics)

	var got boardDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "b3" || got.Name.ValueString() != "media" {
		t.Fatalf("lookup of MEDIA = %s %q, want b3 with its actual name", got.ID, got.Name.ValueString())
	}
}

func TestBoardDataSourcePrefersTheExactName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsOutOfOrder)

	resp := readDataSource(t, &boardDataSource{client: fake.client(t)}, boardLookup("Infra"))
	requireNoErrors(t, resp.Diagnostics)

	var got boardDataSourceModel
	requireNoErrors(t, resp.State.Get(context.Background(), &got))
	if got.ID.ValueString() != "b2" {
		t.Fatalf("lookup of Infra = %s, want the exact match b2", got.ID)
	}
}

func TestBoardDataSourceRefusesAnAmbiguousName(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, boardsOutOfOrder)

	resp := readDataSource(t, &boardDataSource{client: fake.client(t)}, boardLookup("INFRA"))

	requireErrorContaining(t, resp.Diagnostics, "Ambiguous board name")
	requireAttributeError(t, resp.Diagnostics, "name")
}
