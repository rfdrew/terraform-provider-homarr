package provider

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These tests replay what Terraform does to homarr_user state written by
// provider 0.2.0 on the first run after upgrading: upgrade the stored JSON,
// refresh it, and plan against an unchanged configuration.

// userStateV020 is homarr_user state as provider 0.2.0 wrote it.
const userStateV020 = `{"id":"u1","username":"alice","password":"Correct-Horse-1","email":null,"group_ids":null,` +
	`"home_board_id":null,"mobile_home_board_id":null,"provider_name":"credentials"}`

// proposedNewState builds the proposed new state Terraform core sends with
// PlanResourceChange: the configured value of every attribute, and the prior
// value of a computed one the configuration leaves null.
func proposedNewState(t *testing.T, block *tfprotov6.SchemaBlock, typ tftypes.Type, prior, config map[string]tftypes.Value) tfprotov6.DynamicValue {
	t.Helper()
	proposed := map[string]tftypes.Value{}
	for _, attr := range block.Attributes {
		value, configured := config[attr.Name]
		if configured && !value.IsNull() {
			proposed[attr.Name] = value
			continue
		}
		if attr.Computed {
			proposed[attr.Name] = prior[attr.Name]
		}
	}
	return boardProtocolValue(t, typ, proposed)
}

// The configuration predates default_search_engine_id, which homarr_user owns
// outright, so the first plan after upgrading clears a search engine picked in
// the UI. CHANGELOG.md documents that under the breaking changes; this pins
// it, and that nothing else changes or is replaced.
func TestUserUpgradeFromV020PlansOnlyToClearTheSearchEngine(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/users/u1", 200,
		strings.Replace(userDetailJSON, `"provider"`, `"defaultSearchEngineId":"se1","provider"`, 1))
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	h := newIdentityHarness(t, fake)
	typ := h.resourceType(t, "homarr_user")

	upgraded, err := h.server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "homarr_user",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: []byte(userStateV020)},
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, upgraded.Diagnostics)

	read, err := h.server.ReadResource(ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     "homarr_user",
		CurrentState: upgraded.UpgradedState,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, read.Diagnostics)
	prior := decodeObject(t, read.NewState, typ)
	if got := stringAttr(t, prior, "default_search_engine_id"); got != "se1" {
		t.Fatalf("refreshed default_search_engine_id = %q, want se1 from Homarr", got)
	}

	config := map[string]tftypes.Value{"username": tfString("alice"), "password": tfString("Correct-Horse-1")}
	configValue := boardProtocolValue(t, typ, config)
	proposed := proposedNewState(t, h.schemas.ResourceSchemas["homarr_user"].Block, typ, prior, config)
	plan, err := h.server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "homarr_user",
		PriorState:       read.NewState,
		ProposedNewState: &proposed,
		Config:           &configValue,
		PriorIdentity:    read.NewIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, plan.Diagnostics)
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("upgrading planned a replacement: %v", plan.RequiresReplace)
	}

	planned := decodeObject(t, plan.PlannedState, typ)
	var changed []string
	for name, value := range planned {
		if value.IsKnown() && !value.Equal(prior[name]) {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	if len(changed) != 1 || changed[0] != "default_search_engine_id" || !planned["default_search_engine_id"].IsNull() {
		t.Fatalf("attributes planned to change = %v (default_search_engine_id %v), want only "+
			"default_search_engine_id, cleared", changed, planned["default_search_engine_id"])
	}
}
