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
