# Design a template board in the Homarr UI once, then stamp out copies of it,
# tiles included. The template can be any board the API key can view.
data "homarr_board" "template" {
  name = "team-template"
}

resource "homarr_board_copy" "team_a" {
  source_board_id = data.homarr_board.template.id
  name            = "team-a"

  # Inherited from the template when left out. A copy of a public template set
  # to false is public for a moment, until the visibility change lands.
  is_public = false
}

# A copy is a snapshot: later edits to the template are not carried over. Copies
# of a template managed in this configuration would be replaced whenever the
# template is (a column_count change, say), losing everything changed on them in
# the UI since. ignore_changes keeps them.
resource "homarr_board" "template" {
  name         = "ops-template"
  column_count = 12
}

resource "homarr_board_copy" "ops" {
  for_each = toset(["ops-eu", "ops-us"])

  source_board_id = homarr_board.template.id
  name            = each.key

  lifecycle {
    ignore_changes = [source_board_id]
  }
}

# The inherited appearance can be overridden like any other board's.
resource "homarr_board_settings" "team_a" {
  board_id   = homarr_board_copy.team_a.id
  page_title = "Team A"
}
