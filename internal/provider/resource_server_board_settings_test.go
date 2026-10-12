package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// serverBoardSettingsBoards declares the boards the server board settings test
// points at. Homarr requires instance home boards to be public, and the
// throwaway instance's onboarding board is private.
const serverBoardSettingsBoards = `
resource "homarr_board" "server_public" {
  name         = "tfaccserverpublic"
  column_count = 10
  is_public    = true
}

resource "homarr_board" "server_private" {
  name         = "tfaccserverprivate"
  column_count = 10
  is_public    = false
}
`

// serverBoardSettingsPrivateBoard is serverBoardSettingsBoards without the
// public board, for the steps that delete it while a home board still points
// at it.
const serverBoardSettingsPrivateBoard = `
resource "homarr_board" "server_private" {
  name         = "tfaccserverprivate"
  column_count = 10
  is_public    = false
}
`

// testAccStoredHomeBoardsChecked reports whether the Homarr under test checks
// the stored home boards on every change, as the release after 2.4.1 does,
// rather than only the ones a change sends. It sends an empty patch, which
// changes nothing, and must be called while a home board is known to dangle.
// Asking Homarr is more reliable than comparing versions, since a development
// build may report the release it started from.
func testAccStoredHomeBoardsChecked() (bool, error) {
	homarr, err := client.New(client.Options{BaseURL: os.Getenv("HOMARR_URL"), APIKey: os.Getenv("HOMARR_API_KEY")})
	if err != nil {
		return false, err
	}
	_, err = homarr.UpdateServerBoardSettings(context.Background(), nil)
	if err == nil {
		return false, nil
	}
	if _, refused := refusedHomeBoardIDs(err); refused {
		return true, nil
	}
	return false, err
}

// TestAccServerBoardSettingsResource takes over the instance-wide settings of
// the test instance. Its later steps delete a board while the desktop home
// board still points at it, as destroying a configuration holding both would,
// and check what happens to the next change: Homarr 2.4.1 accepts it, and the
// release after it refuses it with the stored-board error. The last step
// clears both home boards, so the test leaves no dangling id behind.
func TestAccServerBoardSettingsResource(t *testing.T) {
	// deletedBoardID is the public board's id, kept for after it is deleted.
	var deletedBoardID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// A private board is refused, and the error names the attribute.
				Config: providerConfig + serverBoardSettingsBoards + `
resource "homarr_server_board_settings" "this" {
  home_board_id = homarr_board.server_private.id
}
`,
				ExpectError: regexpMustCompile(`Home board is not a public board`),
			},
			{
				// Both slots on a public board.
				Config: providerConfig + serverBoardSettingsBoards + `
resource "homarr_server_board_settings" "this" {
  home_board_id            = homarr_board.server_public.id
  mobile_home_board_id     = homarr_board.server_public.id
  enable_status_by_default = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "id", "board"),
					resource.TestCheckResourceAttrPair(
						"homarr_server_board_settings.this", "home_board_id",
						"homarr_board.server_public", "id",
					),
					resource.TestCheckResourceAttrPair(
						"homarr_server_board_settings.this", "mobile_home_board_id",
						"homarr_board.server_public", "id",
					),
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "enable_status_by_default", "true"),
				),
			},
			{
				// Only a flag: the home boards are left out of the patch and
				// adopted as Homarr has them.
				Config: providerConfig + serverBoardSettingsBoards + `
resource "homarr_server_board_settings" "this" {
  force_disable_status = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "force_disable_status", "true"),
					resource.TestCheckResourceAttrPair(
						"homarr_server_board_settings.this", "home_board_id",
						"homarr_board.server_public", "id",
					),
				),
			},
			{
				// "" clears a slot; the data source reports it as null.
				Config: providerConfig + serverBoardSettingsBoards + `
resource "homarr_server_board_settings" "this" {
  home_board_id        = homarr_board.server_public.id
  mobile_home_board_id = ""
  force_disable_status = false
}

data "homarr_server_board_settings" "this" {
  depends_on = [homarr_server_board_settings.this]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "mobile_home_board_id", ""),
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "force_disable_status", "false"),
					resource.TestCheckResourceAttrPair(
						"data.homarr_server_board_settings.this", "home_board_id",
						"homarr_board.server_public", "id",
					),
					resource.TestCheckNoResourceAttr("data.homarr_server_board_settings.this", "mobile_home_board_id"),
				),
			},
			{
				// Import adopts the settings; a cleared slot reads back as null
				// rather than the "" that cleared it.
				ResourceName:            "homarr_server_board_settings.this",
				ImportState:             true,
				ImportStateId:           "board",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"mobile_home_board_id"},
			},
			{
				// Stop managing the home boards, which stay on the public board.
				// This has to be its own step: removing the board in the same
				// apply would delete it before this change is sent.
				Config: providerConfig + serverBoardSettingsBoards + `
resource "homarr_server_board_settings" "this" {
  force_disable_status = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"homarr_server_board_settings.this", "home_board_id",
						"homarr_board.server_public", "id",
					),
					func(state *terraform.State) error {
						rs, ok := state.RootModule().Resources["homarr_board.server_public"]
						if !ok {
							return fmt.Errorf("resource homarr_board.server_public not found in state")
						}
						deletedBoardID = rs.Primary.ID
						return nil
					},
				),
			},
			{
				// Delete the board the desktop home board points at. Homarr
				// leaves its id behind, and a refresh reports it unchanged.
				Config: providerConfig + serverBoardSettingsPrivateBoard + `
resource "homarr_server_board_settings" "this" {
  force_disable_status = true
}
`,
				Check: resource.TestCheckResourceAttrPtr("homarr_server_board_settings.this", "home_board_id", &deletedBoardID),
			},
			{
				// Homarr 2.4.1 and earlier check only the home boards a change
				// sends, so a change to a flag still goes through.
				SkipFunc: testAccStoredHomeBoardsChecked,
				Config: providerConfig + serverBoardSettingsPrivateBoard + `
resource "homarr_server_board_settings" "this" {
  force_disable_status = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "force_disable_status", "false"),
					resource.TestCheckResourceAttrPtr("homarr_server_board_settings.this", "home_board_id", &deletedBoardID),
				),
			},
			{
				// The release after 2.4.1 checks the stored result, so the same
				// change is refused, and the error blames the unmanaged slot.
				SkipFunc: func() (bool, error) {
					checked, err := testAccStoredHomeBoardsChecked()
					return !checked, err
				},
				Config: providerConfig + serverBoardSettingsPrivateBoard + `
resource "homarr_server_board_settings" "this" {
  force_disable_status = false
}
`,
				ExpectError: regexpMustCompile(`Stored home board is not a public board`),
			},
			{
				// Clearing both slots is accepted by every release, and leaves no
				// dangling id behind.
				Config: providerConfig + serverBoardSettingsPrivateBoard + `
resource "homarr_server_board_settings" "this" {
  home_board_id        = ""
  mobile_home_board_id = ""
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "home_board_id", ""),
					resource.TestCheckResourceAttr("homarr_server_board_settings.this", "mobile_home_board_id", ""),
				),
			},
		},
	})
}
