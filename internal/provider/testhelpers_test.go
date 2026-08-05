package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// importBoardIDWithColumnCount builds the `<board_id>,<column_count>` import id
// that homarr_board requires, reading the board id out of the current state.
func importBoardIDWithColumnCount(resourceName, columnCount string) func(*terraform.State) (string, error) {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("resource %s not found in state", resourceName)
		}
		return rs.Primary.ID + "," + columnCount, nil
	}
}

// checkImportedBoard asserts that an imported board carries the expected name
// and the column count supplied through the import id.
func checkImportedBoard(expectedName string) func([]*terraform.InstanceState) error {
	return func(states []*terraform.InstanceState) error {
		if len(states) != 1 {
			return fmt.Errorf("expected exactly one imported instance, got %d", len(states))
		}
		attrs := states[0].Attributes
		if got := attrs["name"]; got != expectedName {
			return fmt.Errorf("imported board name = %q, want %q", got, expectedName)
		}
		if got := attrs["column_count"]; got != "12" {
			return fmt.Errorf("imported board column_count = %q, want %q", got, "12")
		}
		return nil
	}
}
