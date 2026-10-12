resource "homarr_board" "shared" {
  name         = "shared"
  column_count = 10
  is_public    = true
}

resource "random_password" "viewer" {
  length           = 32
  override_special = "!()-_=+"
}

resource "homarr_user" "viewer" {
  # Renaming the user or changing its email is applied in place.
  username = "viewer"
  password = random_password.viewer.result
  email    = "viewer@example.com"

  home_board_id = homarr_board.shared.id

  # A base64 data: URL of a PNG, JPEG, GIF or WebP picture of at most 350,000
  # characters. Leave it out to keep a picture uploaded in the UI; "" removes it.
  image = "data:image/png;base64,${filebase64("${path.module}/viewer.png")}"

  # Once set, the header layout is managed as a whole: a zone left out is empty.
  header = {
    visible        = true
    search_display = "icon"
    logo_display   = "logoAndText"
    left           = [{ builtin = "logo" }]
    center         = [{ builtin = "search" }]
    right = [
      { board_id = homarr_board.shared.id },
      { builtin = "themeToggle" },
      { builtin = "user" },
    ]
  }
}

# With Terraform 1.11 or newer, password_wo keeps the password out of the plan
# and the state. It is sent at creation and whenever password_wo_version changes.
variable "operator_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "homarr_user" "operator" {
  username            = "operator"
  password_wo         = var.operator_password
  password_wo_version = 1

  # Group membership is applied at creation only: Homarr exposes group
  # management solely over tRPC, so the provider can neither read it back nor
  # change it later.
  group_ids = ["hgw6bvj9r0ynmhqoi7ke91b8"]
}
