# Look up an existing app by name...
data "homarr_app" "grafana" {
  name = "Grafana"
}

# ...or by id.
data "homarr_app" "by_id" {
  id = "k723ruvksaauzjvapxpmt9f6"
}

output "grafana_href" {
  value = data.homarr_app.grafana.href
}
