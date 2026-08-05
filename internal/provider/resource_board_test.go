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
