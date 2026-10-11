resource "homarr_board" "landing" {
  name         = "landing"
  column_count = 10
  is_public    = true
}

# Instance-wide board defaults. This is a singleton, so declare at most one.
# Leaving a home board out keeps whatever Homarr has; set it to "" to clear it.
resource "homarr_server_board_settings" "this" {
  home_board_id        = homarr_board.landing.id
  mobile_home_board_id = ""

  enable_status_by_default = true
  force_disable_status     = false
}
