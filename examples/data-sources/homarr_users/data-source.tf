data "homarr_users" "all" {}

# Resolve the id of an account that already exists, without importing it.
locals {
  admin_id = one([for user in data.homarr_users.all.users : user.id if user.name == "admin"])
}

output "admin_id" {
  value = local.admin_id
}
