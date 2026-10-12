resource "homarr_invite" "onboarding" {
  expiration_date = "2027-06-01T00:00:00Z"
}

# The token is returned by Homarr exactly once, at creation, and is stored in
# state. Assemble the link users follow from the id and the token.
output "invite_url" {
  value     = "https://homarr.example.com/auth/invite/${homarr_invite.onboarding.id}?token=${homarr_invite.onboarding.token}"
  sensitive = true
}

# An invite works once: registering through it deletes it, and the next apply
# creates a new one. To hand out a fresh link on a schedule instead, derive the
# expiration from a rotating timestamp: changing it replaces the invite.
resource "time_rotating" "weekly" {
  rotation_days = 7
}

resource "homarr_invite" "weekly" {
  expiration_date = time_rotating.weekly.rotation_rfc3339
}
