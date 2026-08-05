resource "homarr_board" "infra" {
  name         = "infra"
  column_count = 12
  is_public    = true
}

# Appearance and behaviour of a board. Note that this resource is write-only:
# Homarr has no endpoint to read these values back, so changes made in the UI are
# not detected as drift.
resource "homarr_board_settings" "infra" {
  board_id = homarr_board.infra.id

  page_title        = "Infrastructure"
  meta_title        = "Infrastructure — example.com"
  logo_image_url    = "https://example.com/logo.svg"
  favicon_image_url = "https://example.com/favicon.ico"

  background_image_url        = "https://example.com/background.jpg"
  background_image_attachment = "fixed"
  background_image_repeat     = "no-repeat"
  background_image_size       = "cover"

  primary_color   = "#fa5252"
  secondary_color = "#fd7e14"
  opacity         = 80
  item_radius     = "lg"

  disable_status = false

  custom_css = <<-CSS
    .homarr-board {
      letter-spacing: 0.01em;
    }
  CSS
}
