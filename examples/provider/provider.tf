terraform {
  required_providers {
    homarr = {
      source  = "rfdrew/homarr"
      version = "~> 0.1"
    }
  }
}

# Credentials are best supplied through the environment so they stay out of the
# configuration: HOMARR_URL and HOMARR_API_KEY.
provider "homarr" {
  url     = "https://homarr.example.com"
  api_key = var.homarr_api_key
}

variable "homarr_api_key" {
  description = "Homarr API key in <id>.<token> form, from Management > Tools > API."
  type        = string
  sensitive   = true
}
