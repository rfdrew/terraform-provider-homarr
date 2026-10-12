# Requires an admin API key.
data "homarr_server_board_settings" "this" {}

resource "random_password" "alice" {
  length           = 32
  override_special = "!()-_=+"
}

# Pin alice to the instance's default home board, following it on every apply,
# without taking over the instance-wide settings. This overrides any home board
# of her groups; leave home_board_id out to get Homarr's own fallback instead,
# which tries those before the instance default. A board deleted since keeps its
# id in the settings, and the apply then fails on it.
resource "homarr_user" "alice" {
  username      = "alice"
  password      = random_password.alice.result
  home_board_id = data.homarr_server_board_settings.this.home_board_id
}
