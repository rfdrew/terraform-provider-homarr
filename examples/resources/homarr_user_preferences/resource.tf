# Attach preferences to an account this provider did not create — an LDAP or
# OIDC user, the administrator made during onboarding, or anyone added in the
# Homarr UI.
# Usernames are unique only per auth provider, so name the provider too.
data "homarr_user" "jdoe" {
  username      = "jdoe"
  provider_name = "ldap"
}

resource "homarr_user_preferences" "jdoe" {
  user_id = data.homarr_user.jdoe.id

  color_scheme      = "dark"
  byte_unit_system  = "binary"
  first_day_of_week = 1
  home_board_id     = homarr_board.infra.id

  # Homarr sets a profile picture for LDAP and OIDC accounts too. "" removes it.
  image = "data:image/png;base64,${filebase64("${path.module}/jdoe.png")}"
}

# Without user_id, the preferences are those of the user behind the API key.
# Any key may manage its own, so this works with a non-admin key too.
resource "homarr_user_preferences" "me" {
  color_scheme = "light"

  header = {
    visible        = true
    search_display = "input"
    logo_display   = "logo"
    left           = [{ builtin = "logo" }, { builtin = "boardSwitcher" }]
    center         = [{ builtin = "search" }]
    right          = [{ builtin = "settings" }, { builtin = "user" }]
  }
}

# Accounts this provider creates carry the same attributes on homarr_user
# itself; do not manage one user with both resources.
resource "homarr_user" "service" {
  username     = "service"
  password     = var.service_password
  color_scheme = "light"
}
