package provider

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

func TestInvitesDataSourceListsInvitesWithoutTokens(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/invites", 200, `[`+
		`{"id":"old","expirationDate":"2020-01-01T00:00:00.000Z","creator":{"id":"u1","name":"admin"}},`+
		`{"id":"new","expirationDate":"2099-01-01T02:00:00.000+02:00","creator":{"id":"u2","name":null}}`+
		`]`)
	d := &invitesDataSource{client: fake.client(t)}

	var got invitesDataSourceModel
	requireNoErrors(t, readDataSourceForTest(t, d, &invitesDataSourceModel{}, &got))

	if len(got.Invites) != 2 {
		t.Fatalf("invites = %+v, want both invites in Homarr's order", got.Invites)
	}
	old, upcoming := got.Invites[0], got.Invites[1]
	if old.ID.ValueString() != "old" || old.CreatorID.ValueString() != "u1" || old.CreatorName.ValueString() != "admin" {
		t.Errorf("first invite = %+v, want old, created by u1 admin", old)
	}
	// Reported as homarr_invite stores an imported invite: RFC 3339 in UTC.
	if old.ExpirationDate.ValueString() != "2020-01-01T00:00:00Z" ||
		upcoming.ExpirationDate.ValueString() != "2099-01-01T00:00:00Z" {
		t.Errorf("expiration dates = %v, %v; want RFC 3339 in UTC", old.ExpirationDate, upcoming.ExpirationDate)
	}
	if !old.Expired.ValueBool() || upcoming.Expired.ValueBool() {
		t.Errorf("expired = %v, %v; want true for 2020 and false for 2099", old.Expired, upcoming.Expired)
	}
	if !upcoming.CreatorName.IsNull() {
		t.Errorf("creator_name = %v, want null for a creator without a name", upcoming.CreatorName)
	}
}

func TestInvitesDataSourceReportsAnEmptyList(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/invites", 200, `[]`)
	d := &invitesDataSource{client: fake.client(t)}

	var got invitesDataSourceModel
	requireNoErrors(t, readDataSourceForTest(t, d, &invitesDataSourceModel{}, &got))
	if got.Invites == nil || len(got.Invites) != 0 {
		t.Fatalf("invites = %#v, want an empty list rather than null", got.Invites)
	}
}

func TestInvitesDataSourceExplainsADisabledCredentialsProvider(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/invites", 403, `{"message":"Credentials provider is disabled","code":"FORBIDDEN"}`)
	d := &invitesDataSource{client: fake.client(t)}

	var got invitesDataSourceModel
	requireErrorContaining(t, readDataSourceForTest(t, d, &invitesDataSourceModel{}, &got),
		"Credentials provider is disabled")
}

// Homarr refuses an invite once its expiration is earlier than now, so at the
// very instant it is still usable.
func TestInviteSummaryExpiryMatchesHomarrsComparison(t *testing.T) {
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	invite := &client.Invite{ID: "i", ExpirationDate: "2027-01-01T00:00:00.000Z"}

	for now, want := range map[time.Time]bool{
		at.Add(-time.Second): false,
		at:                   false,
		at.Add(time.Second):  true,
	} {
		if got := inviteSummaryFromAPI(invite, now).Expired; !got.Equal(types.BoolValue(want)) {
			t.Errorf("expired at %s = %v, want %v", now.Format(time.RFC3339), got, want)
		}
	}

	if got := inviteSummaryFromAPI(&client.Invite{ID: "i", ExpirationDate: "garbage"}, at); !got.Expired.IsNull() ||
		got.ExpirationDate.ValueString() != "garbage" {
		t.Errorf("an unparseable date gave expiration_date = %v, expired = %v; want it passed through and null",
			got.ExpirationDate, got.Expired)
	}
}
