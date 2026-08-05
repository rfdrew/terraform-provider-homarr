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
  username = "viewer"
  password = random_password.viewer.result
  email    = "viewer@example.com"

  home_board_id = homarr_board.shared.id
}

# Group membership is applied at creation only: Homarr exposes group management
# solely over tRPC, so the provider can neither read it back nor change it later.
resource "homarr_user" "operator" {
  username  = "operator"
  password  = random_password.viewer.result
  group_ids = ["hgw6bvj9r0ynmhqoi7ke91b8"]
}
