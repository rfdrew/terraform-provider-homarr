# The user the provider's API key belongs to. Any key may read it.
data "homarr_current_user" "me" {}

output "api_key_owner" {
  value = "${data.homarr_current_user.me.username} (${data.homarr_current_user.me.provider_name})"
}

# Boards the API key's own user created.
data "homarr_boards" "all" {}

output "my_boards" {
  value = [for board in data.homarr_boards.all.boards : board.name if board.creator_id == data.homarr_current_user.me.id]
}
