# Requires Homarr 2.1.0 or newer. Integrations cannot be listed over REST, so
# copy the id from the integration's edit page: /manage/integrations/edit/<id>.
variable "sonarr_integration_id" {
  type = string
}

# Every read runs a live connection test against Sonarr, so keep the data
# source inside a check block: a failure there is a warning, not an error.
check "sonarr_reachable" {
  data "homarr_integration_connection" "sonarr" {
    integration_id = var.sonarr_integration_id
  }

  assert {
    condition     = data.homarr_integration_connection.sonarr.success
    error_message = "Homarr cannot connect to Sonarr: ${coalesce(data.homarr_integration_connection.sonarr.error, "unknown")}."
  }
}
