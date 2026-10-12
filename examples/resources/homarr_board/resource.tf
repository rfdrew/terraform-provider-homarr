resource "homarr_board" "infra" {
  name         = "infra"
  column_count = 12
  is_public    = false
}

# Board names double as URL slugs, so only letters, digits, hyphens and
# underscores are accepted — "infra core" would be rejected by Homarr. Names are
# unique regardless of case, so "Infra" next to "infra" is refused before the
# board is created; the plan already warns about it.
resource "homarr_board" "public_status" {
  name         = "status-page"
  column_count = 6
  is_public    = true
}

# is_home and is_mobile_home select the home board of the user behind the API
# key. Setting one to false clears that slot again (Homarr 2.3.0 or newer); to
# move the flag, set it on another board instead.
resource "homarr_board" "landing" {
  name           = "landing"
  column_count   = 10
  is_public      = true
  is_home        = true
  is_mobile_home = true
}

# The base layout id is returned only when a board is created. The
# homarr_board_add_item action takes it as layout_id to put a tile at a given
# position on the desktop layout.
output "landing_base_layout_id" {
  value = homarr_board.landing.base_layout_id
}
