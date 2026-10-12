package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	acc "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/querycheck/queryfilter"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// runList runs a list resource through the protocol server and collects every
// result it streams.
func (h identityHarness) runList(t *testing.T, typeName string, includeResource bool, limit int64) []tfprotov6.ListResourceResult {
	t.Helper()
	ctx := context.Background()
	s, ok := h.schemas.ListResourceSchemas[typeName]
	if !ok {
		t.Fatalf("no list resource schema for %s", typeName)
	}
	config := boardProtocolValue(t, s.ValueType(), nil)
	stream, err := h.server.ListResource(ctx, &tfprotov6.ListResourceRequest{
		TypeName:        typeName,
		Config:          &config,
		IncludeResource: includeResource,
		Limit:           limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	var results []tfprotov6.ListResourceResult
	for result := range stream.Results {
		results = append(results, result)
	}
	return results
}

func requireNoListErrors(t *testing.T, results []tfprotov6.ListResourceResult) {
	t.Helper()
	for _, r := range results {
		requireNoProtocolErrors(t, r.Diagnostics)
	}
}

// The provider lists exactly the resources whose objects `terraform query`
// can import, and the type names are those of the resources.
func TestListResourceSchemas(t *testing.T) {
	h := newIdentityHarness(t, newFakeHomarr())
	want := []string{"homarr_app", "homarr_invite", "homarr_user"}
	if len(h.schemas.ListResourceSchemas) != len(want) {
		t.Fatalf("list resources = %v, want %v", h.schemas.ListResourceSchemas, want)
	}
	for _, typeName := range want {
		s, ok := h.schemas.ListResourceSchemas[typeName]
		if !ok {
			t.Fatalf("no list resource %s", typeName)
		}
		if len(s.Block.Attributes) != 0 {
			t.Fatalf("%s takes arguments: %v", typeName, s.Block.Attributes)
		}
		if _, ok := h.identities[typeName]; !ok {
			t.Fatalf("%s is listed, but its resource has no identity to import by", typeName)
		}
	}
}

func TestAppListReportsIdentitiesAndNames(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/apps", 200, `[`+
		`{"id":"a1","name":"Grafana","iconUrl":"https://icons.test/grafana.svg","href":"https://grafana.test","pingUrl":null,"description":null},`+
		`{"id":"a2","name":"Home Assistant","iconUrl":"https://icons.test/ha.svg","href":null,"pingUrl":null,"description":"Lights"}]`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_app", false, 0)
	requireNoListErrors(t, results)
	if len(results) != 2 {
		t.Fatalf("listed %d apps, want 2", len(results))
	}
	for i, want := range []struct{ id, name string }{{"a1", "Grafana"}, {"a2", "Home Assistant"}} {
		h.requireIdentity(t, "homarr_app", results[i].Identity, "id", want.id)
		if results[i].DisplayName != want.name {
			t.Fatalf("result %d display name = %q, want %q", i, results[i].DisplayName, want.name)
		}
		if results[i].Resource != nil {
			t.Fatalf("result %d carries a resource although include_resource was not set", i)
		}
	}
}

// With include_resource the listing's own fields fill the resource, exactly
// as a refresh after an import would, and nothing more is requested.
func TestAppListIncludesTheResource(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/apps", 200, `[{"id":"a1","name":"Grafana","iconUrl":"https://icons.test/grafana.svg",`+
		`"href":"https://grafana.test","pingUrl":null,"description":"Dashboards"}]`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_app", true, 0)
	requireNoListErrors(t, results)
	if len(results) != 1 {
		t.Fatalf("listed %d apps, want 1", len(results))
	}
	attrs := decodeObject(t, results[0].Resource, h.resourceType(t, "homarr_app"))
	for name, want := range map[string]string{
		"id": "a1", "name": "Grafana", "icon_url": "https://icons.test/grafana.svg",
		"href": "https://grafana.test", "description": "Dashboards", "ping_url": "",
	} {
		if got := stringAttr(t, attrs, name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if fake.called("GET /api/apps/a1") {
		t.Fatal("read the app again although the listing held every attribute")
	}
}

// A key without app-modify-all lists the apps it may select, as homarr_apps
// does.
func TestAppListFallsBackToSelectableApps(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/apps", 403, `{"message":"Permission denied","code":"FORBIDDEN"}`)
	fake.reply("GET /api/apps/selectable", 200, `[{"id":"a1","name":"Grafana","iconUrl":"https://icons.test/grafana.svg"}]`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_app", false, 0)
	requireNoListErrors(t, results)
	if len(results) != 1 {
		t.Fatalf("listed %d apps, want 1", len(results))
	}
	h.requireIdentity(t, "homarr_app", results[0].Identity, "id", "a1")
}

func TestListStopsAtTheLimit(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/apps", 200, `[{"id":"a1","name":"a","iconUrl":"i"},{"id":"a2","name":"b","iconUrl":"i"},{"id":"a3","name":"c","iconUrl":"i"}]`)
	h := newIdentityHarness(t, fake)

	if results := h.runList(t, "homarr_app", false, 2); len(results) != 2 {
		t.Fatalf("listed %d apps with a limit of 2", len(results))
	}
}

func TestListReportsAFailedListing(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/apps", 500, `{"message":"database is locked","code":"INTERNAL_SERVER_ERROR"}`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_app", false, 0)
	if len(results) != 1 {
		t.Fatalf("got %d results, want one carrying the error", len(results))
	}
	requireProtocolErrorContaining(t, results[0].Diagnostics, "database is locked")
}

// Only credentials accounts are listed — homarr_user refuses to import any
// other — sorted by name with nameless accounts last, as homarr_users sorts.
func TestUserListReportsCredentialsUsersByName(t *testing.T) {
	fake := newFakeHomarr()
	fake.handle("GET /api/users/selectable", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("providers") != "credentials" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"expected providers=credentials","code":"BAD_REQUEST"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"u3","name":null},{"id":"u2","name":"bob"},{"id":"u1","name":"alice"}]`))
	})
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_user", false, 0)
	requireNoListErrors(t, results)
	want := []struct{ id, name string }{{"u1", "alice"}, {"u2", "bob"}, {"u3", "u3"}}
	if len(results) != len(want) {
		t.Fatalf("listed %d users, want %d", len(results), len(want))
	}
	for i, w := range want {
		h.requireIdentity(t, "homarr_user", results[i].Identity, "id", w.id)
		if results[i].DisplayName != w.name {
			t.Fatalf("result %d display name = %q, want %q", i, results[i].DisplayName, w.name)
		}
	}
	if fake.called("GET /api/users/u1") {
		t.Fatal("read a user although include_resource was not set")
	}
}

// With include_resource each user is read the way Terraform reads one after an
// import; one deleted in between is left out, and the limit counts only the
// users actually reported.
func TestUserListIncludesTheResource(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/selectable", 200, `[{"id":"u1","name":"alice"},{"id":"u2","name":"bob"},{"id":"u3","name":"carol"}]`)
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"alice","email":"alice@example.test","provider":"credentials",`+
		`"colorScheme":"dark","byteUnitSystem":"si","firstDayOfWeek":1}`)
	// u2 was deleted after the listing, so GET /api/users/u2 answers 404.
	fake.reply("GET /api/users/u3", 200, `{"id":"u3","name":"carol","provider":"credentials","colorScheme":"light","byteUnitSystem":"binary"}`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_user", true, 2)
	requireNoListErrors(t, results)
	if len(results) != 2 {
		t.Fatalf("listed %d users, want 2", len(results))
	}
	typ := h.resourceType(t, "homarr_user")
	for i, want := range []struct{ id, username, colorScheme string }{{"u1", "alice", "dark"}, {"u3", "carol", "light"}} {
		h.requireIdentity(t, "homarr_user", results[i].Identity, "id", want.id)
		attrs := decodeObject(t, results[i].Resource, typ)
		if got := stringAttr(t, attrs, "username"); got != want.username {
			t.Fatalf("result %d username = %q, want %q", i, got, want.username)
		}
		if got := stringAttr(t, attrs, "color_scheme"); got != want.colorScheme {
			t.Fatalf("result %d color_scheme = %q, want %q", i, got, want.colorScheme)
		}
		if got := stringAttr(t, attrs, "provider_name"); got != "credentials" {
			t.Fatalf("result %d provider_name = %q, want credentials", i, got)
		}
		if !attrs["password"].IsNull() || !attrs["password_wo"].IsNull() {
			t.Fatalf("result %d reports a password", i)
		}
	}
	if got := stringAttr(t, decodeObject(t, results[0].Resource, typ), "email"); got != "alice@example.test" {
		t.Fatalf("email = %q, want alice@example.test", got)
	}
}

func TestInviteListNamesInvitesByCreatorAndExpiry(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/invites", 200, `[`+
		`{"id":"i1","expirationDate":"2001-01-01T00:00:00.000Z","creator":{"id":"u1","name":"admin"}},`+
		`{"id":"i2","expirationDate":"2099-01-01T02:00:00.000+02:00","creator":{"id":"u2","name":null}}]`)
	h := newIdentityHarness(t, fake)

	results := h.runList(t, "homarr_invite", true, 0)
	requireNoListErrors(t, results)
	if len(results) != 2 {
		t.Fatalf("listed %d invites, want 2", len(results))
	}
	typ := h.resourceType(t, "homarr_invite")
	for i, want := range []struct{ id, name, expiration string }{
		{"i1", "Invite by admin, expired 2001-01-01T00:00:00Z", "2001-01-01T00:00:00Z"},
		{"i2", "Invite by u2, expires 2099-01-01T00:00:00Z", "2099-01-01T00:00:00Z"},
	} {
		h.requireIdentity(t, "homarr_invite", results[i].Identity, "id", want.id)
		if results[i].DisplayName != want.name {
			t.Fatalf("result %d display name = %q, want %q", i, results[i].DisplayName, want.name)
		}
		attrs := decodeObject(t, results[i].Resource, typ)
		if got := stringAttr(t, attrs, "expiration_date"); got != want.expiration {
			t.Fatalf("result %d expiration_date = %q, want %q", i, got, want.expiration)
		}
		if !attrs["token"].IsNull() {
			t.Fatalf("result %d reports a token", i)
		}
	}
}

// --- acceptance ---------------------------------------------------------------

func TestAccListResources(t *testing.T) {
	acc.Test(t, acc.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			// List resources and `terraform query` arrived in Terraform 1.14.
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []acc.TestStep{
			{
				Config: providerConfig + `
resource "homarr_app" "listed" {
  name     = "tfacc-listed"
  icon_url = "` + testIconURL + `"
  href     = "https://example.com"
}

resource "homarr_user" "listed" {
  username = "tfacclisteduser"
  password = "TfProvider!23"
}

resource "homarr_invite" "listed" {
  expiration_date = "2099-01-01T00:00:00Z"
}
`,
			},
			{
				// The query file is read together with the configuration above,
				// which already configures the provider.
				Query: true,
				Config: `
list "homarr_app" "all" {
  provider         = homarr
  include_resource = true
}

list "homarr_user" "all" {
  provider         = homarr
  include_resource = true
}

list "homarr_invite" "all" {
  provider = homarr
}
`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectResourceKnownValues(
						"homarr_app.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tfacc-listed")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("href"), KnownValue: knownvalue.StringExact("https://example.com")},
							{Path: tfjsonpath.New("icon_url"), KnownValue: knownvalue.StringExact(testIconURL)},
						},
					),
					querycheck.ExpectResourceKnownValues(
						"homarr_user.all",
						queryfilter.ByDisplayName(knownvalue.StringExact("tfacclisteduser")),
						[]querycheck.KnownValueCheck{
							{Path: tfjsonpath.New("provider_name"), KnownValue: knownvalue.StringExact("credentials")},
							{Path: tfjsonpath.New("password"), KnownValue: knownvalue.Null()},
						},
					),
					// The bootstrap administrator signs in with credentials too.
					querycheck.ExpectLengthAtLeast("homarr_user.all", 2),
					querycheck.ExpectLengthAtLeast("homarr_invite.all", 1),
				},
			},
		},
	})
}
