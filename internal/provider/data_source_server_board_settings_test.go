package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func emptyServerBoardSettingsConfig() *serverBoardSettingsDataSourceModel {
	return &serverBoardSettingsDataSourceModel{
		HomeBoardID:           types.StringNull(),
		MobileHomeBoardID:     types.StringNull(),
		EnableStatusByDefault: types.BoolNull(),
		ForceDisableStatus:    types.BoolNull(),
	}
}

func TestServerBoardSettingsDataSourceReadsEveryField(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/settings/board", 200, `{"homeBoardId":"H","mobileHomeBoardId":null,`+
		`"enableStatusByDefault":true,"forceDisableStatus":true}`)
	d := &serverBoardSettingsDataSource{client: fake.client(t)}

	var got serverBoardSettingsDataSourceModel
	requireNoErrors(t, readDataSourceForTest(t, d, emptyServerBoardSettingsConfig(), &got))

	if got.HomeBoardID.ValueString() != "H" || !got.MobileHomeBoardID.IsNull() ||
		!got.EnableStatusByDefault.ValueBool() || !got.ForceDisableStatus.ValueBool() {
		t.Fatalf("settings = %+v, want H, null, true, true", got)
	}
}

func TestServerBoardSettingsDataSourceReportsANonAdminKey(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/settings/board", 403, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	d := &serverBoardSettingsDataSource{client: fake.client(t)}

	var got serverBoardSettingsDataSourceModel
	requireErrorContaining(t, readDataSourceForTest(t, d, emptyServerBoardSettingsConfig(), &got),
		"Unable to read Homarr server board settings")
}

// The test instance's settings are whatever the other tests left, so only the
// shape is checked; TestAccServerBoardSettingsResource checks the values.
func TestAccServerBoardSettingsDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "homarr_server_board_settings" "this" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.homarr_server_board_settings.this", "enable_status_by_default"),
					resource.TestCheckResourceAttrSet("data.homarr_server_board_settings.this", "force_disable_status"),
				),
			},
		},
	})
}
