data "homarr_apps" "all" {}

output "app_names" {
  value = [for app in data.homarr_apps.all.apps : app.name]
}

# Handy for finding apps that have no health check configured.
output "apps_without_ping" {
  value = [for app in data.homarr_apps.all.apps : app.name if app.ping_url == null]
}
