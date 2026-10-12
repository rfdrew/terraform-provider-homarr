package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// configureWithVersion runs the real Configure against a fake Homarr that
// reports version from GET /api/info.
func configureWithVersion(t *testing.T, version string) *provider.ConfigureResponse {
	t.Helper()
	fake := newFakeHomarr()
	fake.reply("GET /api/info", http.StatusOK, `{"version":"`+version+`"}`)

	p := &homarrProvider{version: "test", transport: fake}
	var schemaResp provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &schemaResp)

	config := tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw: tftypes.NewValue(schemaResp.Schema.Type().TerraformType(context.Background()), map[string]tftypes.Value{
			"url":                  tftypes.NewValue(tftypes.String, "https://homarr.example.com"),
			"api_key":              tftypes.NewValue(tftypes.String, "id.token"),
			"insecure_skip_verify": tftypes.NewValue(tftypes.Bool, nil),
			"timeout_seconds":      tftypes.NewValue(tftypes.Number, nil),
		}),
	}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, resp)
	return resp
}

// Every kind of provider-level object gets the client: an action or list
// resource that is left out fails with "Unconfigured Homarr provider" on every
// use, and nothing short of a real Terraform run would notice.
func TestConfigureHandsTheClientToEveryObjectKind(t *testing.T) {
	resp := configureWithVersion(t, testedHomarrVersion)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	for name, data := range map[string]any{
		"DataSourceData":   resp.DataSourceData,
		"ResourceData":     resp.ResourceData,
		"ListResourceData": resp.ListResourceData,
		"ActionData":       resp.ActionData,
	} {
		if data == nil {
			t.Errorf("Configure left %s unset", name)
		}
	}
	if resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("tested version %s should not warn: %v", testedHomarrVersion, resp.Diagnostics)
	}
}

func TestConfigureWarnsAboutAnotherMajorVersion(t *testing.T) {
	cases := map[string]bool{
		"1.77.2":        true,
		"3.0.0":         true,
		"2.3.0":         false,
		"2.5.0":         false,
		"2.4.1-beta.1":  false,
		"dev-1a2b3c4d5": false,
	}
	for version, wantWarning := range cases {
		t.Run(version, func(t *testing.T) {
			resp := configureWithVersion(t, version)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected error: %v", resp.Diagnostics)
			}
			warned := false
			for _, d := range resp.Diagnostics.Warnings() {
				if strings.Contains(d.Summary(), "Untested Homarr version") {
					warned = true
				}
			}
			if warned != wantWarning {
				t.Errorf("version %q: warning = %v, want %v", version, warned, wantWarning)
			}
		})
	}
}
