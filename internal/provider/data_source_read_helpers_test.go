package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// dataSourceSchemaForTest returns a data source's schema.
func dataSourceSchemaForTest(t *testing.T, d datasource.DataSource) schema.Schema {
	t.Helper()
	var resp datasource.SchemaResponse
	d.Schema(context.Background(), datasource.SchemaRequest{}, &resp)
	return resp.Schema
}

// readDataSourceForTest runs a data source's Read as Terraform would, with
// config as its configuration — a model whose computed attributes are null —
// and decodes the resulting state into out unless the read failed.
func readDataSourceForTest(t *testing.T, d datasource.DataSource, config, out any) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()

	s := dataSourceSchemaForTest(t, d)

	configured := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	if diags := configured.Set(ctx, config); diags.HasError() {
		t.Fatalf("building configuration: %v", diags)
	}

	resp := datasource.ReadResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configured.Raw}}, &resp)
	if !resp.Diagnostics.HasError() {
		requireNoErrors(t, resp.State.Get(ctx, out))
	}
	return resp.Diagnostics
}
