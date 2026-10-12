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

func TestAccBoardResource_baseLayoutIDAndImportByName(t *testing.T) {
	var layoutID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// POST /api/boards returns the base layout's id, and only then.
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfacclayout"
  column_count = 12
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("homarr_board.test", "base_layout_id"),
					captureAttr("homarr_board.test", "base_layout_id", &layoutID),
				),
			},
			{
				// No REST read returns it again, so it has to survive updates
				// from state.
				Config: providerConfig + `
resource "homarr_board" "test" {
  name         = "tfacclayout2"
  column_count = 12
  is_public    = true
}
`,
				Check: resource.TestCheckResourceAttrPtr("homarr_board.test", "base_layout_id", &layoutID),
			},
			{
				// Import by name, matched case-insensitively. The layout id
				// cannot be recovered by an import.
				ResourceName:            "homarr_board.test",
				ImportState:             true,
				ImportStateId:           "name=TFACCLAYOUT2,12",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"base_layout_id"},
				ImportStateCheck:        checkImportedBoardWithoutLayoutID("tfacclayout2"),
			},
		},
	})
}

func TestAccBoardResource_nameCheck(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfaccnamecase"
  column_count = 10
}
`,
			},
			{
				// Homarr's REST create would accept a name differing only in
				// case; the provider refuses it right before creating. The
				// plan only warns.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfaccnamecase"
  column_count = 10
}

resource "homarr_board" "second" {
  name         = "TFACCNAMECASE"
  column_count = 10
}
`,
				ExpectError: regexpMustCompile(`Board name already taken`),
			},
			{
				// Replacing a board keeps its name; the board being replaced
				// must not count as a clash.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfaccnamecase"
  column_count = 12
}
`,
				Check: resource.TestCheckResourceAttr("homarr_board.first", "column_count", "12"),
			},
			{
				// A tainted board is replaced without a plan against its
				// prior state, so the plan sees its own board under the same
				// name. That is a warning, and the replacement goes ahead.
				Config: providerConfig + `
resource "homarr_board" "first" {
  name         = "tfaccnamecase"
  column_count = 12
}
`,
				Taint: []string{"homarr_board.first"},
				Check: resource.TestCheckResourceAttr("homarr_board.first", "name", "tfaccnamecase"),
			},
		},
	})
}

// A name freed earlier in the same apply can be taken by a new board, although
// the plan still sees the old board holding it.
func TestAccBoardResource_nameFreedInTheSameApply(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_board" "old" {
  name         = "tfaccnamefreed"
  column_count = 10
}
`,
			},
			{
				Config: providerConfig + `
resource "homarr_board" "old" {
  name         = "tfaccnamefreed_old"
  column_count = 10
}

resource "homarr_board" "new" {
  name         = "tfaccnamefreed"
  column_count = 10

  depends_on = [homarr_board.old]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_board.old", "name", "tfaccnamefreed_old"),
					resource.TestCheckResourceAttr("homarr_board.new", "name", "tfaccnamefreed"),
				),
			},
		},
	})
}
