package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccUserDataSourceFixture is a user with an email, preferences and a
// header holding a board shortcut, for the data sources to read back.
const testAccUserDataSourceFixture = `
resource "homarr_board" "dsuser" {
  name         = "tfaccdsuser"
  column_count = 10
  is_public    = true
}

resource "homarr_user" "dsuser" {
  username          = "tfaccdsuser"
  password          = "TfProvider!23"
  email             = "tfacc-dsuser@example.com"
  home_board_id     = homarr_board.dsuser.id
  color_scheme      = "dark"
  first_day_of_week = 1

  header = {
    visible        = true
    search_display = "icon"
    logo_display   = "logo"
    left           = [{ builtin = "logo" }]
    right = [
      { board_id = homarr_board.dsuser.id },
      { builtin = "user" },
    ]
  }
}
`

func TestAccUserDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// By id, and by name in another case narrowed to the
				// credentials provider: both find the same account.
				Config: providerConfig + testAccUserDataSourceFixture + `
data "homarr_user" "by_id" {
  id = homarr_user.dsuser.id
}

data "homarr_user" "by_name" {
  username      = upper(homarr_user.dsuser.username)
  provider_name = "credentials"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "username", "tfaccdsuser"),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "email", "tfacc-dsuser@example.com"),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "provider_name", "credentials"),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "color_scheme", "dark"),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "first_day_of_week", "1"),
					resource.TestCheckNoResourceAttr("data.homarr_user.by_id", "mobile_home_board_id"),
					resource.TestCheckResourceAttrPair(
						"data.homarr_user.by_id", "home_board_id",
						"homarr_board.dsuser", "id",
					),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "header.search_display", "icon"),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "header.left.#", "1"),
					// An empty zone is [] here, unlike in the resource.
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "header.center.#", "0"),
					resource.TestCheckResourceAttrPair(
						"data.homarr_user.by_id", "header.right.0.board_id",
						"homarr_board.dsuser", "id",
					),
					resource.TestCheckResourceAttr("data.homarr_user.by_id", "header.right.1.builtin", "user"),
					resource.TestCheckResourceAttrPair(
						"data.homarr_user.by_name", "id",
						"homarr_user.dsuser", "id",
					),
					// The name is reported as Homarr stores it.
					resource.TestCheckResourceAttr("data.homarr_user.by_name", "username", "tfaccdsuser"),
				),
			},
			{
				// The header read here has the type the resources take, so it
				// can be copied onto another user as a whole.
				Config: providerConfig + testAccUserDataSourceFixture + `
data "homarr_user" "by_id" {
  id = homarr_user.dsuser.id
}

resource "homarr_user" "copy" {
  username = "tfaccdsusercopy"
  password = "TfProvider!23"
  header   = data.homarr_user.by_id.header
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.copy", "header.search_display", "icon"),
					resource.TestCheckResourceAttr("homarr_user.copy", "header.center.#", "0"),
					resource.TestCheckResourceAttrPair(
						"homarr_user.copy", "header.right.0.board_id",
						"homarr_board.dsuser", "id",
					),
				),
			},
		},
	})
}

func TestAccUserDataSource_notFound(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "homarr_user" "missing" {
  username = "tfaccnosuchuser"
}
`,
				ExpectError: regexpMustCompile(`No Homarr user is named "tfaccnosuchuser"`),
			},
			{
				Config: providerConfig + `
data "homarr_user" "missing" {
  id = "tfaccnosuchid"
}
`,
				ExpectError: regexpMustCompile(`Homarr has no user with id tfaccnosuchid`),
			},
		},
	})
}

func TestAccUserDataSource_invalidLookup(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "homarr_user" "both" {
  id       = "abc"
  username = "admin"
}
`,
				ExpectError: regexpMustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: providerConfig + `
data "homarr_user" "provider_with_id" {
  id            = "abc"
  provider_name = "oidc"
}
`,
				ExpectError: regexpMustCompile(`Invalid Attribute Combination`),
			},
		},
	})
}

func TestAccCurrentUserDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The API key belongs to the administrator `make homarr-up`
				// creates during onboarding, a credentials account.
				Config: providerConfig + `
data "homarr_current_user" "me" {}

data "homarr_user" "me" {
  id = data.homarr_current_user.me.id
}

data "homarr_user_preferences" "me" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.homarr_current_user.me", "id"),
					resource.TestCheckResourceAttrSet("data.homarr_current_user.me", "username"),
					resource.TestCheckResourceAttr("data.homarr_current_user.me", "provider_name", "credentials"),
					resource.TestCheckResourceAttrSet("data.homarr_current_user.me", "color_scheme"),
					resource.TestCheckResourceAttrSet("data.homarr_current_user.me", "header.right.#"),
					resource.TestCheckResourceAttrPair(
						"data.homarr_current_user.me", "username",
						"data.homarr_user.me", "username",
					),
					resource.TestCheckResourceAttrPair(
						"data.homarr_current_user.me", "id",
						"data.homarr_user_preferences.me", "user_id",
					),
					resource.TestCheckResourceAttrPair(
						"data.homarr_current_user.me", "color_scheme",
						"data.homarr_user_preferences.me", "color_scheme",
					),
				),
			},
		},
	})
}

func TestAccUserPreferencesDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "dsprefs" {
  username         = "tfaccdsprefs"
  password         = "TfProvider!23"
  color_scheme     = "light"
  byte_unit_system = "decimal"
  ddg_bangs        = false
}

data "homarr_user_preferences" "dsprefs" {
  user_id = homarr_user.dsprefs.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"data.homarr_user_preferences.dsprefs", "user_id",
						"homarr_user.dsprefs", "id",
					),
					resource.TestCheckResourceAttrPair(
						"data.homarr_user_preferences.dsprefs", "id",
						"homarr_user.dsprefs", "id",
					),
					resource.TestCheckResourceAttr("data.homarr_user_preferences.dsprefs", "color_scheme", "light"),
					resource.TestCheckResourceAttr("data.homarr_user_preferences.dsprefs", "byte_unit_system", "decimal"),
					resource.TestCheckResourceAttr("data.homarr_user_preferences.dsprefs", "ddg_bangs", "false"),
					// A reference the user does not have is null, not "".
					resource.TestCheckNoResourceAttr("data.homarr_user_preferences.dsprefs", "home_board_id"),
					resource.TestCheckResourceAttrSet("data.homarr_user_preferences.dsprefs", "header.visible"),
				),
			},
		},
	})
}

func TestAccUsersDataSource_providerName(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "listed" {
  username = "tfacclisted"
  password = "TfProvider!23"
}

data "homarr_users" "all" {
  depends_on = [homarr_user.listed]
}

data "homarr_users" "credentials" {
  provider_name = "credentials"
  depends_on    = [homarr_user.listed]
}

data "homarr_users" "oidc" {
  provider_name = "oidc"
  depends_on    = [homarr_user.listed]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemNestedAttrs("data.homarr_users.all", "users.*", map[string]string{
						"name":          "tfacclisted",
						"provider_name": "credentials",
					}),
					resource.TestCheckTypeSetElemNestedAttrs("data.homarr_users.credentials", "users.*", map[string]string{
						"name":          "tfacclisted",
						"provider_name": "credentials",
					}),
					// The test instance has no OIDC users.
					resource.TestCheckResourceAttr("data.homarr_users.oidc", "users.#", "0"),
				),
			},
		},
	})
}
