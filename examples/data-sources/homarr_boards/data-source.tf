data "homarr_boards" "all" {}

# Boards are sorted by name, so the output is stable between runs.
output "public_boards" {
  value = [for board in data.homarr_boards.all.boards : board.name if board.is_public]
}
