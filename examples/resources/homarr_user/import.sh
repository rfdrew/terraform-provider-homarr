# Users are imported by id, discoverable with the homarr_users data source, or
# by name as "username=<name>". Only accounts that sign in with a Homarr
# password can be imported; manage LDAP and OIDC users with
# homarr_user_preferences. The password cannot be read from Homarr, so the first
# plan after import will propose setting it to whatever your configuration
# specifies — with password_wo, once password_wo_version is set.
#
# Group memberships cannot be read either, so after an import the provider
# knows of none, and a configured group_ids plans a replacement, which deletes
# and recreates the account. Leave group_ids out of imported users.
terraform import homarr_user.viewer hn8u8h18nvpxbvjryfdng4jt

terraform import homarr_user.viewer username=viewer
