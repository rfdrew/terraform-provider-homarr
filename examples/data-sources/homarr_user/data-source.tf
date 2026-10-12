# Look up an account by name. Usernames are unique only per auth provider, so
# provider_name picks the LDAP account if a credentials one shares its name.
data "homarr_user" "jdoe" {
  username      = "jdoe"
  provider_name = "ldap"
}

# ...or by id.
data "homarr_user" "by_id" {
  id = "k723ruvksaauzjvapxpmt9f6"
}

# Manage the preferences of an account Terraform did not create.
resource "homarr_user_preferences" "jdoe" {
  user_id       = data.homarr_user.jdoe.id
  home_board_id = homarr_board.infra.id
}

# The header layout has the shape the resources take, so one user's layout can
# be copied onto another, provided that user can view its board shortcuts.
resource "homarr_user" "intern" {
  username = "intern"
  password = var.intern_password
  header   = data.homarr_user.jdoe.header
}

output "jdoe_email" {
  value = data.homarr_user.jdoe.email
}
