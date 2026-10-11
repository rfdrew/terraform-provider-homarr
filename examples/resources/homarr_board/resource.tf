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
# key. Setting one to false clears that slot again (Homarr 2.3.0 or newer); to
# move the flag, set it on another board instead.
resource "homarr_board" "landing" {
  name           = "landing"
  column_count   = 10
  is_public      = true
  is_home        = true
  is_mobile_home = true
}
