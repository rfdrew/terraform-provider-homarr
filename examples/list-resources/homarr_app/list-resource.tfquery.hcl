# List resources need Terraform 1.14 or newer. Run `terraform query` to see the
# apps, or `terraform query -generate-config-out=apps.tf` to write an import
# block and a resource block for each one.
list "homarr_app" "all" {
  provider = homarr

  # Report every attribute of each app, not just its identity, so the
  # generated resource blocks are complete.
  include_resource = true
}
