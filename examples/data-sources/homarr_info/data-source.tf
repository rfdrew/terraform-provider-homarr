data "homarr_info" "this" {}

output "homarr_version" {
  value = data.homarr_info.this.version
}

# Guard a configuration against instances that are too old.
check "homarr_version_supported" {
  assert {
    condition     = data.homarr_info.this.version != ""
    error_message = "Could not determine the Homarr version."
  }
}
