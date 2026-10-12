# Reading board settings needs *modify* access to the board, not the *view*
# access that listing boards requires, so a read-only API key will not work.
data "homarr_board_settings" "infra" {
  id = homarr_board.infra.id
}

# Copy one board's look onto another.
resource "homarr_board_settings" "staging" {
  board_id      = homarr_board.staging.id
  primary_color = data.homarr_board_settings.infra.primary_color
  item_radius   = data.homarr_board_settings.infra.item_radius
  opacity       = data.homarr_board_settings.infra.opacity
}
