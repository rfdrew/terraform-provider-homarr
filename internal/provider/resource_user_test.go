package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

func TestAccUserResource_profileInPlace(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "profile" {
  username = "tfaccprofile"
  password = "TfProvider!23"
  email    = "tfacc-old@example.com"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.profile", "username", "tfaccprofile"),
					resource.TestCheckResourceAttr("homarr_user.profile", "email", "tfacc-old@example.com"),
				),
			},
			{
				// PUT /api/users/profile renames the user and changes its email
				// without replacing it.
				Config: providerConfig + `
resource "homarr_user" "profile" {
  username = "tfaccprofile2"
  password = "TfProvider!23"
  email    = "tfacc-new@example.com"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.profile", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.profile", "username", "tfaccprofile2"),
					resource.TestCheckResourceAttr("homarr_user.profile", "email", "tfacc-new@example.com"),
				),
			},
			{
				// Import by name resolves to the same credentials account. The
				// password cannot be read back.
				ResourceName:            "homarr_user.profile",
				ImportState:             true,
				ImportStateId:           "username=tfaccprofile2",
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				// Homarr cannot clear an email, so removing it replaces the user.
				Config: providerConfig + `
resource "homarr_user" "profile" {
  username = "tfaccprofile2"
  password = "TfProvider!23"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.profile", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.TestCheckNoResourceAttr("homarr_user.profile", "email"),
			},
		},
	})
}

func TestAccUserResource_invalidEmail(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "bademail" {
  username = "tfaccbademail"
  password = "TfProvider!23"
  email    = "not an email"
}
`,
				ExpectError: regexpMustCompile(`must be an email address as Homarr accepts it`),
			},
		},
	})
}

func TestAccUserResource_header(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// A board shortcut must be viewable by the user whose header it
				// is, which a public board always is.
				Config: providerConfig + `
resource "homarr_board" "header" {
  name         = "tfaccheader"
  column_count = 10
  is_public    = true
}

resource "homarr_user" "header" {
  username = "tfaccheader"
  password = "TfProvider!23"

  header = {
    visible        = true
    search_display = "icon"
    logo_display   = "logo"
    left           = [{ builtin = "logo" }]
    right = [
      { board_id = homarr_board.header.id },
      { builtin = "user" },
    ]
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.header", "header.search_display", "icon"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.logo_display", "logo"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.left.#", "1"),
					resource.TestCheckNoResourceAttr("homarr_user.header", "header.center.#"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.right.#", "2"),
					resource.TestCheckResourceAttrPair(
						"homarr_user.header", "header.right.0.board_id",
						"homarr_board.header", "id",
					),
					resource.TestCheckResourceAttr("homarr_user.header", "header.right.1.builtin", "user"),
				),
			},
			{
				// Rearranging the layout is an in-place update; the emptied left
				// zone must be emptied in Homarr too, not kept by its merge.
				Config: providerConfig + `
resource "homarr_board" "header" {
  name         = "tfaccheader"
  column_count = 10
  is_public    = true
}

resource "homarr_user" "header" {
  username = "tfaccheader"
  password = "TfProvider!23"

  header = {
    visible        = false
    search_display = "input"
    logo_display   = "logoAndText"
    center         = [{ builtin = "search" }, { builtin = "logo" }]
    right          = [{ builtin = "user" }]
  }
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.header", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.header", "header.visible", "false"),
					resource.TestCheckNoResourceAttr("homarr_user.header", "header.left.#"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.center.#", "2"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.center.1.builtin", "logo"),
					resource.TestCheckResourceAttr("homarr_user.header", "header.right.#", "1"),
				),
			},
			{
				// Left out, the header is kept as Homarr has it.
				Config: providerConfig + `
resource "homarr_board" "header" {
  name         = "tfaccheader"
  column_count = 10
  is_public    = true
}

resource "homarr_user" "header" {
  username = "tfaccheader"
  password = "TfProvider!23"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
				Check: resource.TestCheckResourceAttr("homarr_user.header", "header.center.#", "2"),
			},
			{
				ResourceName:            "homarr_user.header",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
		},
	})
}

func TestAccUserResource_invalidHeader(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "badheader" {
  username = "tfaccbadheader"
  password = "TfProvider!23"

  header = {
    visible        = true
    search_display = "input"
    logo_display   = "logo"
    left           = [{ builtin = "logo" }]
  }
}
`,
				ExpectError: regexpMustCompile(`Header without the account menu`),
			},
		},
	})
}

func TestAccUserResource_image(t *testing.T) {
	withImage := func(image string) string {
		return providerConfig + `
resource "homarr_user" "image" {
  username = "tfaccimage"
  password = "TfProvider!23"
  ` + image + `
}
`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The refused URL goes first, before the user exists: the post-test
		// destroy plans the last step's configuration, and one that fails
		// validation would fail it.
		Steps: []resource.TestStep{
			{
				Config:      withImage(`image = "https://example.com/avatar.png"`),
				ExpectError: regexpMustCompile(`base64\s+data:\s+URL`),
			},
			{
				Config: withImage(`image = "` + testImage + `"`),
				Check:  resource.TestCheckResourceAttr("homarr_user.image", "image", testImage),
			},
			{
				// GET /api/users/{id} reports the picture as stored.
				ResourceName:            "homarr_user.image",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"password"},
			},
			{
				// "" removes the picture, in place.
				Config: withImage(`image = ""`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.image", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("homarr_user.image", "image", ""),
			},
			{
				// Left out, the picture is no longer managed.
				Config: withImage(``),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
		},
	})
}

func TestAccUserResource_passwordWriteOnly(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			// Write-only attributes arrived in Terraform 1.11.
			tfversion.SkipBelow(tfversion.Version1_11_0),
		},
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "homarr_user" "wo" {
  username            = "tfaccwriteonly"
  password_wo         = "TfProvider!23"
  password_wo_version = 1
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("homarr_user.wo", "password"),
					resource.TestCheckNoResourceAttr("homarr_user.wo", "password_wo"),
					resource.TestCheckResourceAttr("homarr_user.wo", "password_wo_version", "1"),
				),
			},
			{
				// Bumping the version sends the new password, in place.
				Config: providerConfig + `
resource "homarr_user" "wo" {
  username            = "tfaccwriteonly"
  password_wo         = "TfProvider!45"
  password_wo_version = 2
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.wo", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("homarr_user.wo", "password_wo_version", "2"),
					resource.TestCheckNoResourceAttr("homarr_user.wo", "password_wo"),
				),
			},
			{
				// Without a version change, a different write-only value is not
				// even noticed.
				Config: providerConfig + `
resource "homarr_user" "wo" {
  username            = "tfaccwriteonly"
  password_wo         = "TfProvider!67"
  password_wo_version = 2
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			{
				// Moving back to password is an ordinary password change.
				Config: providerConfig + `
resource "homarr_user" "wo" {
  username = "tfaccwriteonly"
  password = "TfProvider!89"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user.wo", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckNoResourceAttr("homarr_user.wo", "password_wo_version"),
			},
		},
	})
}

func TestAccUserPreferencesResource_apiKeyUser(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Without user_id the resource attaches to the user behind the
				// API key, the bootstrap administrator here. The value set is
				// Homarr's default, so the instance is left as it was.
				Config: providerConfig + `
resource "homarr_user_preferences" "me" {
  color_scheme = "dark"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("homarr_user_preferences.me", "user_id"),
					resource.TestCheckResourceAttrPair(
						"homarr_user_preferences.me", "id",
						"homarr_user_preferences.me", "user_id",
					),
					resource.TestCheckResourceAttr("homarr_user_preferences.me", "color_scheme", "dark"),
					resource.TestCheckResourceAttrSet("homarr_user_preferences.me", "header.right.#"),
				),
			},
			{
				// The id resolved at creation is kept: a change is applied to
				// the same user in place.
				Config: providerConfig + `
resource "homarr_user_preferences" "me" {
  color_scheme = "light"
}
`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("homarr_user_preferences.me", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.TestCheckResourceAttr("homarr_user_preferences.me", "color_scheme", "light"),
			},
			{
				// Back to Homarr's default.
				Config: providerConfig + `
resource "homarr_user_preferences" "me" {
  color_scheme = "dark"
}
`,
				Check: resource.TestCheckResourceAttr("homarr_user_preferences.me", "color_scheme", "dark"),
			},
			{
				ResourceName:      "homarr_user_preferences.me",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
