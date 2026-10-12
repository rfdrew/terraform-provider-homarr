# List resources need Terraform 1.14 or newer. Only accounts that sign in with
# a Homarr password are listed, because homarr_user manages no others.
list "homarr_user" "all" {
  provider = homarr
}

# Reading each user in full needs an admin key and two requests per user. The
# password is never readable, so generated configuration must add it.
list "homarr_user" "detailed" {
  provider         = homarr
  include_resource = true
}
