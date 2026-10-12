package provider

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	acc "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// These tests drive resource identity and the list resources through the
// protocol server Terraform talks to, with the resources and list resources
// exactly as provider.go registers them. Requests that carry no identity are
// what Terraform before 1.12 sends, and what any Terraform sends for state an
// older provider wrote.

// identityHarnessProvider is the homarr provider configured with the
// in-process fake Homarr instead of reaching out over HTTP.
type identityHarnessProvider struct {
	homarrProvider
	client *client.Client
}

var _ fwprovider.ProviderWithListResources = &identityHarnessProvider{}

func (p *identityHarnessProvider) Configure(_ context.Context, _ fwprovider.ConfigureRequest, resp *fwprovider.ConfigureResponse) {
	resp.ResourceData = p.client
	resp.DataSourceData = p.client
	resp.ActionData = p.client
	resp.ListResourceData = p.client
}

// identityHarness is a configured protocol server plus the schemas it serves.
type identityHarness struct {
	server     tfprotov6.ProviderServerWithListResource
	schemas    *tfprotov6.GetProviderSchemaResponse
	identities map[string]*tfprotov6.ResourceIdentitySchema
}

func newIdentityHarness(t *testing.T, fake *fakeHomarr) identityHarness {
	t.Helper()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(&identityHarnessProvider{client: fake.client(t)})()
	if err != nil {
		t.Fatal(err)
	}
	withLists, ok := server.(tfprotov6.ProviderServerWithListResource)
	if !ok {
		t.Fatal("the framework's protocol server does not serve list resources")
	}

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, schemas.Diagnostics)
	identities, err := server.GetResourceIdentitySchemas(ctx, &tfprotov6.GetResourceIdentitySchemasRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, identities.Diagnostics)

	config := boardProtocolValue(t, schemas.Provider.ValueType(), nil)
	resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{TerraformVersion: "1.14.0", Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, resp.Diagnostics)
	return identityHarness{server: withLists, schemas: schemas, identities: identities.IdentitySchemas}
}

func (h identityHarness) resourceType(t *testing.T, typeName string) tftypes.Type {
	t.Helper()
	s, ok := h.schemas.ResourceSchemas[typeName]
	if !ok {
		t.Fatalf("no resource schema for %s", typeName)
	}
	return s.ValueType()
}

func (h identityHarness) identityType(t *testing.T, typeName string) tftypes.Type {
	t.Helper()
	s, ok := h.identities[typeName]
	if !ok {
		t.Fatalf("no identity schema for %s", typeName)
	}
	return s.ValueType()
}

// identityData encodes an identity of typeName holding attribute = id.
func (h identityHarness) identityData(t *testing.T, typeName, attribute, id string) *tfprotov6.ResourceIdentityData {
	t.Helper()
	dv := boardProtocolValue(t, h.identityType(t, typeName), map[string]tftypes.Value{attribute: tfString(id)})
	return &tfprotov6.ResourceIdentityData{IdentityData: &dv}
}

// decodeObject turns a protocol value into its attributes, or nil for null.
func decodeObject(t *testing.T, dv *tfprotov6.DynamicValue, typ tftypes.Type) map[string]tftypes.Value {
	t.Helper()
	if dv == nil {
		t.Fatal("no value")
	}
	value, err := dv.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	if value.IsNull() {
		return nil
	}
	var attrs map[string]tftypes.Value
	if err := value.As(&attrs); err != nil {
		t.Fatal(err)
	}
	return attrs
}

func stringAttr(t *testing.T, attrs map[string]tftypes.Value, name string) string {
	t.Helper()
	v, ok := attrs[name]
	if !ok {
		t.Fatalf("no attribute %q in %v", name, attrs)
	}
	var s string
	if v.IsNull() {
		return ""
	}
	if err := v.As(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (h identityHarness) requireIdentity(t *testing.T, typeName string, identity *tfprotov6.ResourceIdentityData, attribute, want string) {
	t.Helper()
	if identity == nil {
		t.Fatalf("%s: no identity returned, want %s = %q", typeName, attribute, want)
	}
	attrs := decodeObject(t, identity.IdentityData, h.identityType(t, typeName))
	if got := stringAttr(t, attrs, attribute); got != want {
		t.Fatalf("%s: identity %s = %q, want %q", typeName, attribute, got, want)
	}
}

// identityCase is one resource with an identity: the state of an object it
// manages, how the fake Homarr reports that object, and how it reports it gone.
type identityCase struct {
	typeName  string
	attribute string
	id        string
	state     map[string]tftypes.Value
	exists    func(*fakeHomarr)
	gone      func(*fakeHomarr)
}

func identityCases() []identityCase {
	none := func(*fakeHomarr) {}
	return []identityCase{
		{
			typeName: "homarr_app", attribute: "id", id: "a1",
			state: map[string]tftypes.Value{"id": tfString("a1"), "name": tfString("Grafana"), "icon_url": tfString("https://icons.test/grafana.svg")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/apps/a1", 200, `{"id":"a1","name":"Grafana","iconUrl":"https://icons.test/grafana.svg"}`)
			},
			gone: none,
		},
		{
			typeName: "homarr_user", attribute: "id", id: "u1",
			state: map[string]tftypes.Value{"id": tfString("u1"), "username": tfString("alice")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/users/u1", 200, `{"id":"u1","name":"alice","provider":"credentials","colorScheme":"dark","byteUnitSystem":"si"}`)
			},
			gone: none,
		},
		{
			typeName: "homarr_invite", attribute: "id", id: "i1",
			state: map[string]tftypes.Value{"id": tfString("i1"), "expiration_date": tfString("2027-01-01T00:00:00Z")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/invites", 200, `[{"id":"i1","expirationDate":"2027-01-01T00:00:00.000Z","creator":{"id":"u1","name":"admin"}}]`)
			},
			gone: func(f *fakeHomarr) { f.reply("GET /api/invites", 200, `[]`) },
		},
		{
			typeName: "homarr_board_settings", attribute: "board_id", id: "b1",
			state: map[string]tftypes.Value{"id": tfString("b1"), "board_id": tfString("b1")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/boards/b1/settings", 200, `{"id":"b1","name":"infra","backgroundImageAttachment":"fixed",`+
					`"backgroundImageRepeat":"no-repeat","backgroundImageSize":"cover","primaryColor":"#fa5252",`+
					`"secondaryColor":"#fd7e14","opacity":100,"itemRadius":"lg","customCss":"","disableStatus":false}`)
			},
			gone: func(f *fakeHomarr) { f.reply("GET /api/boards", 200, `[]`) },
		},
		{
			typeName: "homarr_user_preferences", attribute: "user_id", id: "u1",
			state: map[string]tftypes.Value{"id": tfString("u1"), "user_id": tfString("u1")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/users/preferences", 200, `{"userId":"u1","colorScheme":"dark","byteUnitSystem":"si"}`)
				f.reply("GET /api/users/u1", 200, `{"id":"u1","name":"alice","provider":"oidc"}`)
			},
			gone: none,
		},
		{
			typeName: "homarr_board_copy", attribute: "id", id: "c1",
			state: map[string]tftypes.Value{"id": tfString("c1"), "source_board_id": tfString("s1"), "name": tfString("copy")},
			exists: func(f *fakeHomarr) {
				f.reply("GET /api/boards", 200, `[{"id":"c1","name":"copy","isPublic":true,"isHome":false,"isMobileHome":false}]`)
			},
			gone: func(f *fakeHomarr) { f.reply("GET /api/boards", 200, `[]`) },
		},
	}
}

// Every resource with an identity declares it as one id that an import block
// must set. homarr_board and homarr_server_board_settings deliberately have
// none; see resource_identity.go.
func TestResourceIdentitySchemas(t *testing.T) {
	h := newIdentityHarness(t, newFakeHomarr())

	var got []string
	for typeName := range h.identities {
		got = append(got, typeName)
	}
	sort.Strings(got)
	cases := identityCases()
	if len(got) != len(cases) {
		t.Fatalf("resources with an identity = %v, want exactly the %d in identityCases", got, len(cases))
	}
	for _, c := range cases {
		s := h.identities[c.typeName]
		if s == nil || len(s.IdentityAttributes) != 1 {
			t.Fatalf("%s: identity schema = %v, want one attribute", c.typeName, s)
		}
		attr := s.IdentityAttributes[0]
		if attr.Name != c.attribute || !attr.RequiredForImport || !attr.Type.Is(tftypes.String) {
			t.Fatalf("%s: identity attribute = %+v, want a string %q required for import", c.typeName, attr, c.attribute)
		}
	}
	for _, typeName := range []string{"homarr_board", "homarr_server_board_settings"} {
		if _, ok := h.identities[typeName]; ok {
			t.Fatalf("%s has an identity, but its import cannot be expressed as one", typeName)
		}
	}
}

// A refresh reports the identity even when the request carries none.
func TestResourceIdentityIsReportedByRead(t *testing.T) {
	ctx := context.Background()
	for _, c := range identityCases() {
		t.Run(c.typeName, func(t *testing.T) {
			fake := newFakeHomarr()
			c.exists(fake)
			h := newIdentityHarness(t, fake)
			typ := h.resourceType(t, c.typeName)
			state := boardProtocolValue(t, typ, c.state)

			resp, err := h.server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: c.typeName, CurrentState: &state})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, resp.Diagnostics)
			if decodeObject(t, resp.NewState, typ) == nil {
				t.Fatal("the object was dropped from state")
			}
			h.requireIdentity(t, c.typeName, resp.NewIdentity, c.attribute, c.id)

			// A refresh that already has the identity keeps it unchanged; the
			// framework fails one that changes it.
			again, err := h.server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
				TypeName:        c.typeName,
				CurrentState:    resp.NewState,
				CurrentIdentity: resp.NewIdentity,
			})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, again.Diagnostics)
			h.requireIdentity(t, c.typeName, again.NewIdentity, c.attribute, c.id)
		})
	}
}

// A refresh that finds the object gone drops it from state without an error,
// also when state has no identity yet. The framework requires an identity
// after every Read that succeeds, a removal included, so Read must set it
// before it learns the object is gone.
func TestResourceIdentityReadOfAGoneObjectWithoutAPriorIdentity(t *testing.T) {
	ctx := context.Background()
	for _, c := range identityCases() {
		t.Run(c.typeName, func(t *testing.T) {
			fake := newFakeHomarr()
			c.gone(fake)
			h := newIdentityHarness(t, fake)
			typ := h.resourceType(t, c.typeName)
			state := boardProtocolValue(t, typ, c.state)

			resp, err := h.server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{TypeName: c.typeName, CurrentState: &state})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, resp.Diagnostics)
			if attrs := decodeObject(t, resp.NewState, typ); attrs != nil {
				t.Fatalf("a gone object stayed in state: %v", attrs)
			}
		})
	}
}

// An import block's identity (Terraform 1.12+) imports the object it names,
// and the identity comes back unchanged.
func TestResourceImportByIdentity(t *testing.T) {
	ctx := context.Background()
	for _, c := range identityCases() {
		t.Run(c.typeName, func(t *testing.T) {
			fake := newFakeHomarr()
			c.exists(fake)
			h := newIdentityHarness(t, fake)

			resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
				TypeName: c.typeName,
				Identity: h.identityData(t, c.typeName, c.attribute, c.id),
			})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, resp.Diagnostics)
			if len(resp.ImportedResources) != 1 {
				t.Fatalf("imported %d objects, want 1", len(resp.ImportedResources))
			}
			imported := resp.ImportedResources[0]
			attrs := decodeObject(t, imported.State, h.resourceType(t, c.typeName))
			if got := stringAttr(t, attrs, c.attribute); got != c.id {
				t.Fatalf("imported %s = %q, want %q", c.attribute, got, c.id)
			}
			if got := stringAttr(t, attrs, "id"); got != c.id {
				t.Fatalf("imported id = %q, want %q", got, c.id)
			}
			h.requireIdentity(t, c.typeName, imported.Identity, c.attribute, c.id)

			// The refresh Terraform runs right after the import reports the
			// same identity.
			read, err := h.server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
				TypeName:        c.typeName,
				CurrentState:    imported.State,
				CurrentIdentity: imported.Identity,
				Private:         imported.Private,
			})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, read.Diagnostics)
			h.requireIdentity(t, c.typeName, read.NewIdentity, c.attribute, c.id)
		})
	}
}

// An import block whose identity sets an empty id is refused by every resource
// before anything is asked of Homarr, rather than looked up as an id of "".
func TestResourceImportByAnEmptyIdentityIsRefused(t *testing.T) {
	ctx := context.Background()
	for _, c := range identityCases() {
		t.Run(c.typeName, func(t *testing.T) {
			fake := newFakeHomarr()
			h := newIdentityHarness(t, fake)

			resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
				TypeName: c.typeName,
				Identity: h.identityData(t, c.typeName, c.attribute, ""),
			})
			if err != nil {
				t.Fatal(err)
			}
			requireProtocolErrorContaining(t, resp.Diagnostics, "Missing import identity")
			if calls := fake.callLog(); len(calls) != 0 {
				t.Fatalf("an empty identity reached Homarr: %v", calls)
			}
		})
	}
}

// An import id still works, and already yields the identity.
func TestResourceImportByIDSetsTheIdentity(t *testing.T) {
	ctx := context.Background()
	for _, c := range identityCases() {
		t.Run(c.typeName, func(t *testing.T) {
			fake := newFakeHomarr()
			c.exists(fake)
			h := newIdentityHarness(t, fake)

			resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{TypeName: c.typeName, ID: c.id})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, resp.Diagnostics)
			if len(resp.ImportedResources) != 1 {
				t.Fatalf("imported %d objects, want 1", len(resp.ImportedResources))
			}
			h.requireIdentity(t, c.typeName, resp.ImportedResources[0].Identity, c.attribute, c.id)
		})
	}
}

// The identity holds the id an import by name resolved to, not the name.
func TestResourceImportByNameRecordsTheResolvedID(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[{"id":"c1","name":"team-a","isPublic":true,"isHome":false,"isMobileHome":false}]`)
	fake.reply("GET /api/users", 200, `[{"id":"u1","name":"alice"}]`)
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"alice","provider":"credentials"}`)
	h := newIdentityHarness(t, fake)

	for typeName, importID := range map[string]string{
		"homarr_board_copy": "name=team-a",
		"homarr_user":       "username=alice",
	} {
		resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{TypeName: typeName, ID: importID})
		if err != nil {
			t.Fatal(err)
		}
		requireNoProtocolErrors(t, resp.Diagnostics)
		want := map[string]string{"homarr_board_copy": "c1", "homarr_user": "u1"}[typeName]
		h.requireIdentity(t, typeName, resp.ImportedResources[0].Identity, "id", want)
	}
}

// An identity always holds a plain id, so a board copy's identity that happens
// to read like "name=..." is not looked up as a name.
func TestBoardCopyImportByIdentityIsNeverAName(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	h := newIdentityHarness(t, fake)

	resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
		TypeName: "homarr_board_copy",
		Identity: h.identityData(t, "homarr_board_copy", "id", "name=team-a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, resp.Diagnostics)
	attrs := decodeObject(t, resp.ImportedResources[0].State, h.resourceType(t, "homarr_board_copy"))
	if got := stringAttr(t, attrs, "id"); got != "name=team-a" {
		t.Fatalf("imported id = %q, want the identity's value unchanged", got)
	}
	if fake.called("GET /api/boards") {
		t.Fatal("an identity was resolved as a board name")
	}
}

// Importing an LDAP or OIDC account into homarr_user is refused by identity as
// it is by id.
func TestUserImportByIdentityRefusesAnExternalAccount(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200, `{"id":"u1","name":"alice","provider":"ldap"}`)
	h := newIdentityHarness(t, fake)

	resp, err := h.server.ImportResourceState(ctx, &tfprotov6.ImportResourceStateRequest{
		TypeName: "homarr_user",
		Identity: h.identityData(t, "homarr_user", "id", "u1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	requireProtocolErrorContaining(t, resp.Diagnostics, "ldap")
}

// nullIdentity is the empty identity the framework hands a resource's Create.
func nullIdentity(t *testing.T, r resource.ResourceWithIdentity) *tfsdk.ResourceIdentity {
	t.Helper()
	ctx := context.Background()
	var resp resource.IdentitySchemaResponse
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &resp)
	return &tfsdk.ResourceIdentity{
		Schema: resp.IdentitySchema,
		Raw:    tftypes.NewValue(resp.IdentitySchema.Type().TerraformType(ctx), nil),
	}
}

func identityID(t *testing.T, identity *tfsdk.ResourceIdentity, attribute string) string {
	t.Helper()
	var id types.String
	requireNoErrors(t, identity.GetAttribute(context.Background(), path.Root(attribute), &id))
	return id.ValueString()
}

func TestAppCreateSetsTheIdentity(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("POST /api/apps", 200, `{"id":"a1","appId":"a1","name":"Grafana","iconUrl":"https://icons.test/grafana.svg"}`)
	r := &appResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	plan := planOf(t, s, &appModel{
		ID:      types.StringUnknown(),
		Name:    types.StringValue("Grafana"),
		IconURL: types.StringValue("https://icons.test/grafana.svg"),
	})
	resp := resource.CreateResponse{
		State:    tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)},
		Identity: nullIdentity(t, r),
	}
	r.Create(ctx, resource.CreateRequest{Plan: plan, Identity: nullIdentity(t, r)}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	if got := identityID(t, resp.Identity, "id"); got != "a1" {
		t.Fatalf("identity id = %q, want a1", got)
	}
}

// A create that fails after the object came into being keeps it in state,
// tainted, and its identity with it.
func TestInviteCreateKeepsTheIdentityWhenTheReadBackFails(t *testing.T) {
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
	resp := resource.CreateResponse{
		State:    tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)},
		Identity: nullIdentity(t, r),
	}
	r.Create(ctx, resource.CreateRequest{Plan: plan, Identity: nullIdentity(t, r)}, &resp)

	requireErrorContaining(t, resp.Diagnostics, "database is locked")
	if got := identityID(t, resp.Identity, "id"); got != "i1" {
		t.Fatalf("identity id = %q, want i1", got)
	}
}

// homarr_user_preferences without user_id learns the id from Homarr, and the
// identity records that id.
func TestUserPreferencesCreateRecordsTheResolvedUserInTheIdentity(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 200, `{"userId":"u7","colorScheme":"light","byteUnitSystem":"si"}`)
	fake.reply("GET /api/users/u7", 200, `{"id":"u7","name":"admin","provider":"credentials"}`)
	r := &userPreferencesResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	model := userPreferencesCreatePlan(types.StringUnknown())
	resp := resource.CreateResponse{
		State:    tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)},
		Identity: nullIdentity(t, r),
	}
	r.Create(ctx, resource.CreateRequest{
		Plan:     planOf(t, s, &model),
		Config:   configOf(t, s, &model),
		Identity: nullIdentity(t, r),
	}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	if got := identityID(t, resp.Identity, "user_id"); got != "u7" {
		t.Fatalf("identity user_id = %q, want u7", got)
	}
}

// The framework fails a create that returns no identity, so every resource
// with one must set it on its success path too.
func TestCreateSetsTheIdentity(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		r         func(*client.Client) resource.ResourceWithIdentity
		fake      func(*fakeHomarr)
		plan      any
		attribute string
		want      string
	}{
		{
			name: "homarr_user",
			r:    func(c *client.Client) resource.ResourceWithIdentity { return &userResource{client: c} },
			fake: func(f *fakeHomarr) {
				f.replySequence("GET /api/users", `[]`, `[{"id":"u1","name":"alice"}]`)
				f.reply("POST /api/users", 200, ``)
				f.reply("GET /api/users/u1", 200, userDetailJSON)
				f.reply("GET /api/users/preferences", 200, userPreferencesJSON)
			},
			plan:      func() any { m := userCreatePlan(); return &m }(),
			attribute: "id",
			want:      "u1",
		},
		{
			name: "homarr_board_copy",
			r:    func(c *client.Client) resource.ResourceWithIdentity { return &boardCopyResource{client: c} },
			fake: func(f *fakeHomarr) {
				f.reply("POST /api/boards/template/duplicate", 200, `{"boardId":"c1"}`)
				f.reply("GET /api/boards", 200, listedCopy)
			},
			plan:      copyPlan(types.BoolUnknown()),
			attribute: "id",
			want:      "c1",
		},
		{
			name: "homarr_board_settings",
			r:    func(c *client.Client) resource.ResourceWithIdentity { return &boardSettingsResource{client: c} },
			fake: func(f *fakeHomarr) {
				f.reply("GET /api/boards/b1/settings", 200, `{"id":"b1","name":"b","opacity":100}`)
			},
			plan:      func() any { m := boardIDOnlyPlan(); return &m }(),
			attribute: "board_id",
			want:      "b1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			tc.fake(fake)
			r := tc.r(fake.client(t))
			s := resourceSchema(t, r)
			resp := resource.CreateResponse{State: nullStateFor(t, r), Identity: nullIdentity(t, r)}
			r.Create(ctx, resource.CreateRequest{
				Plan:     planOf(t, s, tc.plan),
				Config:   configOf(t, s, tc.plan),
				Identity: nullIdentity(t, r),
			}, &resp)
			requireNoErrors(t, resp.Diagnostics)

			if got := identityID(t, resp.Identity, tc.attribute); got != tc.want {
				t.Fatalf("identity %s = %q, want %q", tc.attribute, got, tc.want)
			}
		})
	}
}

// --- acceptance ---------------------------------------------------------------

// testAccIdentityConfig declares one object of every resource with an
// identity. homarr_board is there only to hold the settings and the copy.
func testAccIdentityConfig() string {
	return providerConfig + `
resource "homarr_app" "identity" {
  name     = "tfacc-identity"
  icon_url = "` + testIconURL + `"
}

resource "homarr_user" "identity" {
  username = "tfaccidentity"
  password = "TfProvider!23"
}

resource "homarr_user_preferences" "identity" {
  user_id      = homarr_user.identity.id
  color_scheme = "dark"
}

resource "homarr_invite" "identity" {
  expiration_date = "2099-01-01T00:00:00Z"
}

resource "homarr_board" "identity" {
  name         = "tfaccidentity"
  column_count = 10
  is_public    = true
}

resource "homarr_board_settings" "identity" {
  board_id   = homarr_board.identity.id
  page_title = "Identity"
}

resource "homarr_board_copy" "identity" {
  source_board_id = homarr_board.identity.id
  name            = "tfaccidentitycopy"
}
`
}

func TestAccResourceIdentity(t *testing.T) {
	// Each resource with an identity, the attribute holding it, and whether
	// the plan right after an import has changes: a user's password and a
	// copy's source cannot be read back, so the next apply records them.
	importSteps := []struct {
		address   string
		attribute string
		changes   bool
	}{
		{"homarr_app.identity", "id", false},
		{"homarr_user.identity", "id", true},
		{"homarr_user_preferences.identity", "user_id", false},
		{"homarr_invite.identity", "id", false},
		{"homarr_board_settings.identity", "board_id", false},
		{"homarr_board_copy.identity", "id", true},
	}

	var checks []statecheck.StateCheck
	for _, s := range importSteps {
		checks = append(checks, statecheck.ExpectIdentityValueMatchesState(s.address, tfjsonpath.New(s.attribute)))
	}
	steps := []acc.TestStep{{
		Config:            testAccIdentityConfig(),
		ConfigStateChecks: checks,
	}}
	for _, s := range importSteps {
		steps = append(steps, acc.TestStep{
			ResourceName:       s.address,
			ImportState:        true,
			ImportStateKind:    acc.ImportBlockWithResourceIdentity,
			ExpectNonEmptyPlan: s.changes,
		})
	}

	acc.Test(t, acc.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			// Resource identity, and import blocks that use one, arrived in
			// Terraform 1.12.
			tfversion.SkipBelow(tfversion.Version1_12_0),
		},
		Steps: steps,
	})
}

// expectPlannedIdentity checks that a plan gives the resource at address an
// identity whose attribute holds the same id as the state attribute of that
// name.
type expectPlannedIdentity struct {
	address, attribute string
}

func (e expectPlannedIdentity) CheckPlan(_ context.Context, req plancheck.CheckPlanRequest, resp *plancheck.CheckPlanResponse) {
	if req.Plan == nil || req.Plan.PlannedValues == nil || req.Plan.PlannedValues.RootModule == nil {
		resp.Error = fmt.Errorf("the plan has no planned values")
		return
	}
	for _, r := range req.Plan.PlannedValues.RootModule.Resources {
		if r.Address != e.address {
			continue
		}
		id, _ := r.IdentityValues[e.attribute].(string)
		if id == "" || id != r.AttributeValues[e.attribute] {
			resp.Error = fmt.Errorf("%s: planned identity %v, want %s = %v", e.address, r.IdentityValues, e.attribute,
				r.AttributeValues[e.attribute])
		}
		return
	}
	resp.Error = fmt.Errorf("%s is not in the plan", e.address)
}

// An import block with an id, rather than an identity, still records the
// identity — the resolved id, for the name forms of the import id.
func TestAccResourceIdentity_importBlockWithID(t *testing.T) {
	acc.Test(t, acc.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_12_0),
		},
		Steps: []acc.TestStep{
			{
				Config: testAccIdentityConfig(),
			},
			{
				ResourceName:       "homarr_user.identity",
				ImportState:        true,
				ImportStateKind:    acc.ImportBlockWithID,
				ImportStateId:      "username=tfaccidentity",
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: acc.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{expectPlannedIdentity{"homarr_user.identity", "id"}},
				},
			},
			{
				ResourceName:       "homarr_board_copy.identity",
				ImportState:        true,
				ImportStateKind:    acc.ImportBlockWithID,
				ImportStateId:      "name=tfaccidentitycopy",
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: acc.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{expectPlannedIdentity{"homarr_board_copy.identity", "id"}},
				},
			},
		},
	})
}
