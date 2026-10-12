# Requires Homarr 2.2.0 or newer and an admin API key.
data "homarr_root_certificate" "corp" {
  file_name = "corp-root-ca.crt"
}

# Warn a month before the CA Homarr trusts for its integrations expires. A
# missing file is not an error: found is false.
check "corp_root_ca" {
  assert {
    condition     = data.homarr_root_certificate.corp.found
    error_message = "Homarr does not trust corp-root-ca.crt."
  }

  # A conditional rather than ||: Terraform before 1.12 evaluates both sides of
  # ||, and timecmp fails on the null valid_to of a missing file.
  assert {
    condition = data.homarr_root_certificate.corp.found ? timecmp(
      data.homarr_root_certificate.corp.valid_to,
      timeadd(plantimestamp(), "720h"),
    ) > 0 : true
    error_message = "corp-root-ca.crt expires within 30 days."
  }
}

# Confirm Homarr holds exactly the file you distributed.
output "corp_root_ca_matches" {
  value = data.homarr_root_certificate.corp.sha256 == sha256(file("${path.module}/corp-root-ca.crt"))
}
