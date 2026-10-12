# The name is matched exactly first, then case-insensitively — the way Homarr
# resolves board URLs — so "Infra" finds a board named "infra".
data "homarr_board" "infra" {
  name = "infra"
}

# Attach settings to a board that was created outside Terraform.
resource "homarr_board_settings" "infra" {
  board_id   = data.homarr_board.infra.id
  page_title = "Infrastructure"
}
