package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// regexpMustCompile keeps ExpectError patterns readable across the test files.
func regexpMustCompile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

func TestAccBoardResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfaccboard"
  column_count = 12
  is_public    = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.test", "name", "tfaccboard"),
					resource.TestCheckResourceAttr("homarr_board.test", "column_count", "12"),
					resource.TestCheckResourceAttr("homarr_board.test", "is_public", "true"),
					resource.TestCheckResourceAttrSet("homarr_board.test", "id"),
					resource.TestCheckResourceAttrSet("homarr_board.test", "creator_id"),
				),
			},
			{
				// Renaming is an in-place update via PATCH /api/boards/{id}/name.
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfaccboard-renamed"
  column_count = 12
  is_public    = true
}
`,
				Check: resource.TestCheckResourceAttr("homarr_board.test", "name", "tfaccboard-renamed"),
			},
			{
				// The column count is unreadable, so import needs it supplied
				// explicitly. ImportStateVerify would compare against a null
				// column_count, hence it stays off here.
				ResourceName:       "homarr_board.test",
				ImportState:        true,
				ImportStateIdFunc:  importBoardIDWithColumnCount("homarr_board.test", "12"),
				ImportStateVerify:  false,
				ImportStateCheck:   checkImportedBoard("tfaccboard-renamed"),
				ImportStatePersist: false,
			},
		},
	})
}

func TestAccBoardResource_rejectsInvalidName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Homarr's board name schema forbids spaces; the provider
				// validates this before any API call is made.
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "not a valid name"
  column_count = 10
}
`,
				// Terraform hard-wraps diagnostic text, so match a fragment that
				// cannot straddle a line break.
				ExpectError: regexpMustCompile(`must contain only ASCII letters, digits, hyphens`),
			},
		},
	})
}

func TestAccBoardResource_homeBoards(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Selecting both home slots happens after creation, through
				// PATCH /api/boards/{id}/home and .../mobile-home.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name           = "tfacchomeone"
  column_count   = 10
  is_public      = true
  is_home        = true
  is_mobile_home = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.first", "is_home", "true"),
					resource.TestCheckResourceAttr("homarr_board.first", "is_mobile_home", "true"),
				),
			},
			{
				// The home board is a per-user singleton: selecting it on a
				// second board has to drop the flag on the first. The first
				// board stops declaring it, otherwise the two would fight over
				// it on every apply.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfacchomeone"
  column_count = 10
  is_public    = true
}

resource "homarr_board" "second" {
  name           = "tfacchometwo"
  column_count   = 10
  is_public      = true
  is_home        = true
  is_mobile_home = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.second", "is_home", "true"),
					resource.TestCheckResourceAttr("homarr_board.second", "is_mobile_home", "true"),
				),
			},
			{
				// The first board's flags are deliberately not asserted above.
				// Dropping the attributes from its configuration produces no
				// diff — an omitted Optional+Computed attribute plans as its
				// prior state — so Terraform never re-reads that board during
				// the apply that moved the flag, and its state still says true.
				// The correction lands on the next refresh, which is what this
				// step exercises.
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.first", "is_home", "false"),
					resource.TestCheckResourceAttr("homarr_board.first", "is_mobile_home", "false"),
					resource.TestCheckResourceAttr("homarr_board.second", "is_home", "true"),
					resource.TestCheckResourceAttr("homarr_board.second", "is_mobile_home", "true"),
				),
			},
			{
				// Clearing goes through PATCH /api/users/preferences, which is
				// the only way to unset a home board; the board-side endpoint
				// can only select one.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfacchomeone"
  column_count = 10
  is_public    = true
}

resource "homarr_board" "second" {
  name           = "tfacchometwo"
  column_count   = 10
  is_public      = true
  is_home        = false
  is_mobile_home = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.second", "is_home", "false"),
					resource.TestCheckResourceAttr("homarr_board.second", "is_mobile_home", "false"),
				),
			},
		},
	})
}

func TestAccBoardResource_rejectsBareImportID(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfaccimport"
  column_count = 10
}
`,
			},
			{
				// A bare id would leave column_count null and make the next plan
				// destroy the board, so the provider refuses it outright.
				ResourceName:  "homarr_board.test",
				ImportState:   true,
				ImportStateId: "some-board-id",
				ExpectError:   regexpMustCompile(`Unexpected import identifier`),
			},
		},
	})
}
