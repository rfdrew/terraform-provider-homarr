data "homarr_info" "this" {}

output "homarr_version" {
  value = data.homarr_info.this.version # e.g. "2.4.1"
}

locals {
  # "2.4.1" becomes [2, 4]; a version that does not parse becomes [0, 0].
  homarr_version = try([for part in slice(split(".", data.homarr_info.this.version), 0, 2) : tonumber(part)], [0, 0])
}

# Guard a configuration against instances that are too old. This provider
# targets Homarr 2.4.1, and features such as the preference attributes need
# 2.3.0 or newer.
check "homarr_version_supported" {
  assert {
    condition     = local.homarr_version[0] > 2 || (local.homarr_version[0] == 2 && local.homarr_version[1] >= 3)
    error_message = "Homarr ${data.homarr_info.this.version} is older than 2.3.0, which this configuration needs."
  }
}
