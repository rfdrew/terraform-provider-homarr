package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccBoardSettingsResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "settings" {
  name         = "tfaccsettings"
  column_count = 10
  is_public    = true
}

resource "homarr_board_settings" "settings" {
  board_id      = homarr_board.settings.id
  page_title    = "Infrastructure"
  primary_color = "#fa5252"
  opacity       = 80
  item_radius   = "md"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "page_title", "Infrastructure"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "primary_color", "#fa5252"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "opacity", "80"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "item_radius", "md"),
					// Undeclared attributes are computed from the read-back, so
					// they carry Homarr's defaults rather than staying null.
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "background_image_size", "cover"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "disable_status", "false"),
					resource.TestCheckResourceAttrSet("homarr_board_settings.settings", "secondary_color"),
				),
			},
			{
				Config: providerConfig + `
resource "homarr_board" "settings" {
  name         = "tfaccsettings"
  column_count = 10
  is_public    = true
}

resource "homarr_board_settings" "settings" {
  board_id      = homarr_board.settings.id
  page_title    = "Infrastructure renamed"
  primary_color = "#fa5252"
  opacity       = 55
  item_radius   = "xl"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "page_title", "Infrastructure renamed"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "opacity", "55"),
					resource.TestCheckResourceAttr("homarr_board_settings.settings", "item_radius", "xl"),
				),
			},
			{
				// Readable since Homarr 2.3.0, so a bare board id now imports
				// cleanly and every attribute verifies against the server.
				ResourceName:      "homarr_board_settings.settings",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccBoardSettingsResource_rejectsFractionalOpacity(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Homarr's schema calls opacity a number but stores an integer
				// column, so a fractional value would be silently truncated and
				// read back different. The attribute is an integer to match.
				Config: providerConfig + `
resource "homarr_board" "frac" {
  name         = "tfaccfrac"
  column_count = 10
}

resource "homarr_board_settings" "frac" {
  board_id = homarr_board.frac.id
  opacity  = 50.5
}
`,
				ExpectError: regexpMustCompile(`[Ii]nvalid|whole number|integer`),
			},
		},
	})
}
