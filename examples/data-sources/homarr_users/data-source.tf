# Any API key may list users: for a key that is not an admin's, the provider
# falls back to the list behind Homarr's user pickers.
data "homarr_users" "all" {}

# Users are sorted by name, then id. Names are unique only per auth provider,
# so an LDAP or OIDC account may share one with a credentials account;
# provider_name tells them apart.
output "usernames" {
  value = [for user in data.homarr_users.all.users : "${user.name} (${user.provider_name})"]
}

output "users_without_email" {
  value = [for user in data.homarr_users.all.users : user.name if user.email == null]
}

# Only the accounts that sign in through OIDC.
data "homarr_users" "oidc" {
  provider_name = "oidc"
}

output "oidc_user_ids" {
  value = data.homarr_users.oidc.users[*].id
}
