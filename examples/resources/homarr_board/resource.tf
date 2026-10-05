resource "homarr_board" "infra" {
  name         = "infra"
  column_count = 12
  is_public    = false
}

# Board names double as URL slugs, so only letters, digits, hyphens and
# underscores are accepted — "infra core" would be rejected by Homarr.
resource "homarr_board" "public_status" {
  name         = "status-page"
  column_count = 6
  is_public    = true
}

# is_home and is_mobile_home select the home board of the user behind the API
# key. They can only be turned on — Homarr has no call that clears a home board
# — so to move the flag you set it on another board, and to drop it entirely you
# set homarr_user.home_board_id to null.
resource "homarr_board" "landing" {
  name           = "landing"
  column_count   = 10
  is_public      = true
  is_home        = true
  is_mobile_home = true
}
