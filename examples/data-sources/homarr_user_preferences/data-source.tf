# Without user_id, the preferences of the user behind the API key, which any
# key may read.
data "homarr_user_preferences" "me" {}

# Naming another user requires an admin key.
data "homarr_user" "jdoe" {
  username = "jdoe"
}

data "homarr_user_preferences" "jdoe" {
  user_id = data.homarr_user.jdoe.id
}

# Give a new account the same look as an existing one.
resource "homarr_user" "intern" {
  username         = "intern"
  password         = var.intern_password
  color_scheme     = data.homarr_user_preferences.jdoe.color_scheme
  byte_unit_system = data.homarr_user_preferences.jdoe.byte_unit_system
  header           = data.homarr_user_preferences.jdoe.header
}

output "my_home_board" {
  # null when the user has no home board.
  value = data.homarr_user_preferences.me.home_board_id
}
