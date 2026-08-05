package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testIconURL = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/homarr.svg"

func TestAccAppResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create with every attribute populated.
				Config: providerConfig + fmt.Sprintf(`
resource "homarr_app" "test" {
  name        = "tfacc-app"
  description = "created by acceptance test"
  icon_url    = %[1]q
  href        = "https://example.com"
  ping_url    = "https://example.com/health"
}
`, testIconURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_app.test", "name", "tfacc-app"),
					resource.TestCheckResourceAttr("homarr_app.test", "description", "created by acceptance test"),
					resource.TestCheckResourceAttr("homarr_app.test", "icon_url", testIconURL),
					resource.TestCheckResourceAttr("homarr_app.test", "href", "https://example.com"),
					resource.TestCheckResourceAttr("homarr_app.test", "ping_url", "https://example.com/health"),
					resource.TestCheckResourceAttrSet("homarr_app.test", "id"),
				),
			},
			{
				// Import must round-trip without producing a diff.
				ResourceName:      "homarr_app.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				// Update every mutable attribute in place.
				Config: providerConfig + fmt.Sprintf(`
resource "homarr_app" "test" {
  name        = "tfacc-app-renamed"
  description = "updated by acceptance test"
  icon_url    = %[1]q
  href        = "https://example.org"
  ping_url    = "https://example.org/health"
}
`, testIconURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_app.test", "name", "tfacc-app-renamed"),
					resource.TestCheckResourceAttr("homarr_app.test", "description", "updated by acceptance test"),
					resource.TestCheckResourceAttr("homarr_app.test", "href", "https://example.org"),
					resource.TestCheckResourceAttr("homarr_app.test", "ping_url", "https://example.org/health"),
				),
			},
			{
				// Clearing the optional attributes must send explicit nulls and
				// read back as null rather than as empty strings.
				Config: providerConfig + fmt.Sprintf(`
resource "homarr_app" "test" {
  name     = "tfacc-app-renamed"
  icon_url = %[1]q
}
`, testIconURL),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("homarr_app.test", "description"),
					resource.TestCheckNoResourceAttr("homarr_app.test", "href"),
					resource.TestCheckNoResourceAttr("homarr_app.test", "ping_url"),
				),
			},
		},
	})
}

func TestAccAppResource_invalidName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
resource "homarr_app" "test" {
  name     = ""
  icon_url = %[1]q
}
`, testIconURL),
				ExpectError: regexpMustCompile(`Attribute name string length must be between 1 and 64`),
			},
		},
	})
}
