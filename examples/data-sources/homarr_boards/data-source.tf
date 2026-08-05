data "homarr_boards" "all" {}

output "public_boards" {
  value = [for board in data.homarr_boards.all.boards : board.name if board.is_public]
}
