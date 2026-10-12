# List resources need Terraform 1.14 or newer. Expired invites are listed too,
# because Homarr never deletes them; their display names say so.
list "homarr_invite" "all" {
  provider = homarr
}
