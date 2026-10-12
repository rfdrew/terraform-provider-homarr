resource "homarr_board" "landing" {
  name         = "landing"
  column_count = 10
  is_public    = true
}

# Instance-wide board defaults. This is a singleton, so declare at most one.
# Leaving a home board out keeps whatever Homarr has; set it to "" to clear it.
#
# Both home boards must be public boards. After onboarding, both point at
# Homarr's private onboarding board, and Homarr releases after 2.4.1 check the
# stored home boards on every change, so manage both of them together.
#
# Destroying this resource does not clear the home boards, and deleting a board
# does not either. Before destroying homarr_board.landing, or this whole
# configuration, set home_board_id to "" and apply, or the instance is left
# with the id of a board that no longer exists.
resource "homarr_server_board_settings" "this" {
  home_board_id        = homarr_board.landing.id
  mobile_home_board_id = ""

  enable_status_by_default = true
  force_disable_status     = false
}
