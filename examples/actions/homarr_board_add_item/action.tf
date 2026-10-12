# Actions need Terraform 1.14 or newer. The action keeps no state: every
# invocation adds another tile, and Terraform never removes it again.
resource "homarr_board" "infra" {
  name         = "infra"
  column_count = 12
}

resource "homarr_app" "grafana" {
  name     = "Grafana"
  icon_url = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/grafana.svg"
  href     = "https://grafana.example.com"

  # Place a tile for the app once, when the app is created. The action reads the
  # app's id, so it has to run after_create; before_create would be a cycle.
  # Destroying the app later leaves the tile behind, pointing at nothing.
  lifecycle {
    action_trigger {
      events  = [after_create]
      actions = [action.homarr_board_add_item.grafana]
    }
  }
}

action "homarr_board_add_item" "grafana" {
  config {
    board_id     = homarr_board.infra.id
    kind         = "app"
    options_json = jsonencode({ appId = homarr_app.grafana.id })
    width        = 2
    height       = 1

    # Optional: move the tile to column 4 of the top row on the desktop layout.
    # The mobile layout keeps the position Homarr picked. base_layout_id is
    # known only for boards this provider created, not for imports or copies.
    layout_id = homarr_board.infra.base_layout_id
    x         = 4
    y         = 0
  }
}

# Widgets without an integration work the same way; this one can be added on
# demand with: terraform apply -invoke=action.homarr_board_add_item.clock
action "homarr_board_add_item" "clock" {
  config {
    board_id     = homarr_board.infra.id
    kind         = "clock"
    options_json = jsonencode({ is24HourFormat = true })
  }
}
