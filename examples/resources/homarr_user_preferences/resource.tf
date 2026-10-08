# Attach preferences to an account this provider did not create — an LDAP or
# OIDC user, the administrator made during onboarding, or anyone added in the
# Homarr UI.
data "homarr_users" "all" {}

locals {
  ldap_user_id = one([for user in data.homarr_users.all.users : user.id if user.name == "jdoe"])
}

resource "homarr_user_preferences" "jdoe" {
  user_id = local.ldap_user_id

  color_scheme      = "dark"
  byte_unit_system  = "binary"
  first_day_of_week = 1
  home_board_id     = homarr_board.infra.id
}

# Accounts this provider creates carry the same attributes on homarr_user
# itself; do not manage one user with both resources.
resource "homarr_user" "service" {
  username     = "service"
  password     = var.service_password
  color_scheme = "light"
}
