# A fully specified app tile.
resource "homarr_app" "grafana" {
  name        = "Grafana"
  description = "Metrics and dashboards"
  icon_url    = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/grafana.svg"
  href        = "https://grafana.example.com"
  ping_url    = "https://grafana.example.com/api/health"
}

# Only name and icon_url are required.
resource "homarr_app" "docs" {
  name     = "Internal docs"
  icon_url = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/bookstack.svg"
  href     = "https://docs.example.com"
}

# Apps are frequently driven from a map so the whole catalogue lives in one place.
locals {
  services = {
    portainer = { url = "https://portainer.example.com", icon = "portainer" }
    checkmk   = { url = "https://checkmk.example.com", icon = "checkmk" }
  }
}

resource "homarr_app" "services" {
  for_each = local.services

  name     = each.key
  icon_url = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/${each.value.icon}.svg"
  href     = each.value.url
  ping_url = each.value.url
}
