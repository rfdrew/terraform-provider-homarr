data "homarr_invites" "all" {}

# Homarr never deletes expired invites on its own.
check "no_expired_invites" {
  assert {
    condition     = length([for invite in data.homarr_invites.all.invites : invite.id if invite.expired]) == 0
    error_message = "Homarr holds expired invites; delete them or let Terraform manage them."
  }
}

output "open_invite_ids" {
  value = [for invite in data.homarr_invites.all.invites : invite.id if !invite.expired]
}
