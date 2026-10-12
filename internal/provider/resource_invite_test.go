package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	acc "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// Homarr returns an invite's token only in the creation response. If reading
// the invite back fails afterwards, the invite still exists, so the id and the
// token must reach state — tainted by the error — rather than be lost.
func TestInviteCreateKeepsTheTokenWhenTheReadBackFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("POST /api/invites", 200, `{"id":"i1","token":"secret"}`)
	fake.reply("GET /api/invites", 500, `{"message":"database is locked","code":"INTERNAL_SERVER_ERROR"}`)
	r := &inviteResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	plan := planOf(t, s, &inviteModel{
		ID:             types.StringUnknown(),
		ExpirationDate: types.StringValue("2027-01-01T00:00:00Z"),
		Token:          types.StringUnknown(),
		CreatorID:      types.StringUnknown(),
		CreatorName:    types.StringUnknown(),
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)

	requireErrorContaining(t, resp.Diagnostics, "database is locked")
	requireErrorContaining(t, resp.Diagnostics, "saved to state")

	var got inviteModel
	requireNoErrors(t, resp.State.Get(ctx, &got))
	if got.ID.ValueString() != "i1" || got.Token.ValueString() != "secret" {
		t.Fatalf("id = %v, token = %v; want the created invite kept in state", got.ID, got.Token)
	}
	if got.ExpirationDate.ValueString() != "2027-01-01T00:00:00Z" {
		t.Fatalf("expiration_date = %v, want the planned value", got.ExpirationDate)
	}
	// Terraform refuses unknown values in state after an apply.
	if !resp.State.Raw.IsFullyKnown() {
		t.Fatalf("state holds unknown values: %v", resp.State.Raw)
	}
}

// --- acceptance ---------------------------------------------------------------

func TestAccInviteResource(t *testing.T) {
	acc.Test(t, acc.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInvitesDestroyed,
		Steps: []acc.TestStep{
			{
				Config: providerConfig + `
resource "homarr_invite" "test" {
  expiration_date = "2099-01-01T00:00:00Z"
}
`,
				Check: acc.ComposeAggregateTestCheckFunc(
					acc.TestCheckResourceAttrSet("homarr_invite.test", "id"),
					acc.TestCheckResourceAttrSet("homarr_invite.test", "token"),
					acc.TestCheckResourceAttrSet("homarr_invite.test", "creator_id"),
					acc.TestCheckResourceAttr("homarr_invite.test", "expiration_date", "2099-01-01T00:00:00Z"),
				),
			},
			{
				// The data source lists the invite, without its token.
				Config: providerConfig + `
resource "homarr_invite" "test" {
  expiration_date = "2099-01-01T00:00:00Z"
}

data "homarr_invites" "all" {
  depends_on = [homarr_invite.test]
}
`,
				Check: acc.ComposeAggregateTestCheckFunc(
					acc.TestCheckTypeSetElemAttrPair("data.homarr_invites.all", "invites.*.id", "homarr_invite.test", "id"),
					acc.TestCheckTypeSetElemNestedAttrs("data.homarr_invites.all", "invites.*", map[string]string{
						"expiration_date": "2099-01-01T00:00:00Z",
						"expired":         "false",
					}),
				),
			},
			{
				// The token cannot be recovered on import — the provider warns
				// about it — so it is the one attribute that does not verify.
				ResourceName:            "homarr_invite.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"token"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("expected exactly one imported invite, got %d", len(states))
					}
					if token, ok := states[0].Attributes["token"]; ok && token != "" {
						return fmt.Errorf("imported token = %q, want none", token)
					}
					return nil
				},
			},
		},
	})
}

// testAccCheckInvitesDestroyed confirms that every homarr_invite in the final
// state is gone from Homarr.
func testAccCheckInvitesDestroyed(state *terraform.State) error {
	homarr, err := client.New(client.Options{BaseURL: os.Getenv("HOMARR_URL"), APIKey: os.Getenv("HOMARR_API_KEY")})
	if err != nil {
		return err
	}
	for _, rs := range state.RootModule().Resources {
		if rs.Type != "homarr_invite" {
			continue
		}
		_, err := homarr.GetInvite(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("invite %s still exists after destroy", rs.Primary.ID)
		}
		if !client.IsNotFound(err) {
			return err
		}
	}
	return nil
}
