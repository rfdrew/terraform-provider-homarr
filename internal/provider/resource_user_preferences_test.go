package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccUserPreferencesResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The account is created by homarr_user but its preferences are
				// managed separately here, which is the shape an LDAP or OIDC
				// user would take — the two resources declare disjoint
				// attributes so they do not fight.
				Config: providerConfig + `
resource "homarr_user" "prefs" {
  username = "tfaccprefs"
  password = "TfProvider!23"
}

resource "homarr_user_preferences" "prefs" {
  user_id           = homarr_user.prefs.id
  color_scheme      = "dark"
  first_day_of_week = 1
  ddg_bangs         = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "color_scheme", "dark"),
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "first_day_of_week", "1"),
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "ddg_bangs", "false"),
					// Undeclared preferences are computed from the read-back.
					resource.TestCheckResourceAttrSet("homarr_user_preferences.prefs", "byte_unit_system"),
					resource.TestCheckResourceAttrPair(
						"homarr_user_preferences.prefs", "user_id",
						"homarr_user.prefs", "id",
					),
				),
			},
			{
				Config: providerConfig + `
resource "homarr_user" "prefs" {
  username = "tfaccprefs"
  password = "TfProvider!23"
}

resource "homarr_user_preferences" "prefs" {
  user_id           = homarr_user.prefs.id
  color_scheme      = "light"
  first_day_of_week = 0
  ddg_bangs         = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "color_scheme", "light"),
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "first_day_of_week", "0"),
					resource.TestCheckResourceAttr("homarr_user_preferences.prefs", "ddg_bangs", "true"),
				),
			},
			{
				ResourceName:      "homarr_user_preferences.prefs",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccUserResource_preferences(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Creation applies the account and every preference in one
				// validated write, so there is no window where the user exists
				// with the wrong settings.
				Config: providerConfig + `
resource "homarr_user" "withprefs" {
  username                      = "tfaccuserprefs"
  password                      = "TfProvider!23"
  color_scheme                  = "dark"
  byte_unit_system              = "binary"
  first_day_of_week             = 1
  enable_right_click_on_widgets = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.withprefs", "color_scheme", "dark"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "byte_unit_system", "binary"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "first_day_of_week", "1"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "enable_right_click_on_widgets", "true"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "provider_name", "credentials"),
				),
			},
			{
				Config: providerConfig + `
resource "homarr_user" "withprefs" {
  username                      = "tfaccuserprefs"
  password                      = "TfProvider!23"
  color_scheme                  = "auto"
  byte_unit_system              = "decimal"
  first_day_of_week             = 6
  enable_right_click_on_widgets = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.withprefs", "color_scheme", "auto"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "byte_unit_system", "decimal"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "first_day_of_week", "6"),
					resource.TestCheckResourceAttr("homarr_user.withprefs", "enable_right_click_on_widgets", "false"),
				),
			},
		},
	})
}
