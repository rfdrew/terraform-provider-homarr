# Users are imported by id, discoverable with the homarr_users data source.
# The password cannot be read from Homarr, so the first plan after import will
# propose setting it to whatever your configuration specifies.
terraform import homarr_user.viewer hn8u8h18nvpxbvjryfdng4jt
