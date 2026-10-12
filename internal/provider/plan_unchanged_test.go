package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// These plans are proposed the way Terraform proposes them: the configuration,
// with the prior value of every optional computed attribute it leaves null —
// except the nested `header`, which Terraform proposes as null. The acceptance
// tests caught every homarr_user and homarr_user_preferences planning an update
// after its own apply because of that.

func dynamicValueFor(t *testing.T, r resource.Resource, model any) *tfprotov6.DynamicValue {
	t.Helper()
	s := resourceSchema(t, r)
	dv, err := tfprotov6.NewDynamicValue(s.Type().TerraformType(context.Background()), stateOf(t, s, model).Raw)
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

func requirePlanEqualsPrior(t *testing.T, resp *tfprotov6.PlanResourceChangeResponse, r resource.Resource, prior *tfprotov6.DynamicValue) {
	t.Helper()
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan failed: %s: %s", d.Summary, d.Detail)
		}
	}
	typ := resourceSchema(t, r).Type().TerraformType(context.Background())
	planned, err := resp.PlannedState.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	want, err := prior.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	if !planned.Equal(want) {
		diffs, _ := planned.Diff(want)
		for _, d := range diffs {
			t.Errorf("planned %s = %v, want the prior %v", d.Path, d.Value1, d.Value2)
		}
	}
}

// A user without a profile picture, whose header layout is not configured.
func TestUserPlanAfterItsOwnApplyIsEmpty(t *testing.T) {
	prior := readUserState()
	prior.Image = types.StringNull()
	config := userConfigOf(prior)
	proposed := prior
	proposed.Header = config.Header

	priorValue := dynamicValueFor(t, &userResource{}, &prior)
	resp, err := protocolServer(t).PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "homarr_user",
		PriorState:       priorValue,
		ProposedNewState: dynamicValueFor(t, &userResource{}, &proposed),
		Config:           dynamicValueFor(t, &userResource{}, &config),
	})
	if err != nil {
		t.Fatal(err)
	}
	requirePlanEqualsPrior(t, resp, &userResource{}, priorValue)
}

func TestUserPreferencesPlanAfterItsOwnApplyIsEmpty(t *testing.T) {
	prior := userPreferencesModel{
		ID:     types.StringValue("u1"),
		UserID: types.StringValue("u1"),
		Image:  types.StringNull(),
		userPreferenceValues: userPreferenceValues{
			HomeBoardID:               types.StringNull(),
			MobileHomeBoardID:         types.StringNull(),
			DefaultSearchEngineID:     types.StringNull(),
			ColorScheme:               types.StringValue("dark"),
			ByteUnitSystem:            types.StringValue("decimal"),
			FirstDayOfWeek:            types.Int64Value(1),
			PingIconsEnabled:          types.BoolValue(false),
			EnableRightClickOnWidgets: types.BoolValue(true),
			OpenSearchInNewTab:        types.BoolValue(true),
			DdgBangs:                  types.BoolValue(true),
			Header:                    headerValueFromAPI(homarrDefaultHeader(), types.ObjectNull(headerAttrTypes)),
		},
	}
	// Only color_scheme is configured.
	config := userPreferencesModel{
		ID:     types.StringNull(),
		UserID: types.StringValue("u1"),
		Image:  types.StringNull(),
		userPreferenceValues: userPreferenceValues{
			HomeBoardID:               types.StringNull(),
			MobileHomeBoardID:         types.StringNull(),
			DefaultSearchEngineID:     types.StringNull(),
			ColorScheme:               types.StringValue("dark"),
			ByteUnitSystem:            types.StringNull(),
			FirstDayOfWeek:            types.Int64Null(),
			PingIconsEnabled:          types.BoolNull(),
			EnableRightClickOnWidgets: types.BoolNull(),
			OpenSearchInNewTab:        types.BoolNull(),
			DdgBangs:                  types.BoolNull(),
			Header:                    types.ObjectNull(headerAttrTypes),
		},
	}
	proposed := prior
	proposed.Header = config.Header

	r := &userPreferencesResource{}
	priorValue := dynamicValueFor(t, r, &prior)
	resp, err := protocolServer(t).PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "homarr_user_preferences",
		PriorState:       priorValue,
		ProposedNewState: dynamicValueFor(t, r, &proposed),
		Config:           dynamicValueFor(t, r, &config),
	})
	if err != nil {
		t.Fatal(err)
	}
	requirePlanEqualsPrior(t, resp, r, priorValue)
}

// A real change keeps the unknowns the framework marked: the values really are
// only known after the apply.
func TestUserPlanWithAChangeKeepsItsUnknowns(t *testing.T) {
	prior := readUserState()
	prior.Image = types.StringNull()
	config := userConfigOf(prior)
	config.Username = types.StringValue("bob")

	planned, _ := planUser(t, prior, config)
	if planned.Username.ValueString() != "bob" || !planned.Image.IsUnknown() {
		t.Fatalf("planned username = %v, image = %v; want the rename with the image left unknown",
			planned.Username, planned.Image)
	}
}

// An attribute configured with a value that is not known yet is a change, even
// when everything else matches the prior state.
func TestKeepUnchangedPlanLeavesAnUnknownConfiguredValueAlone(t *testing.T) {
	s := resourceSchema(t, &userResource{})
	prior := readUserState()
	state := stateOf(t, s, &prior)

	config := userConfigOf(prior)
	config.Username = types.StringUnknown()
	configRaw := stateOf(t, s, &config).Raw

	plan := prior
	plan.Username = types.StringUnknown()
	planRaw := stateOf(t, s, &plan).Raw

	req := resource.ModifyPlanRequest{State: state}
	req.Config.Schema, req.Config.Raw = s, configRaw
	req.Plan.Schema, req.Plan.Raw = s, planRaw
	resp := resource.ModifyPlanResponse{}
	resp.Plan.Schema, resp.Plan.Raw = s, planRaw.Copy()

	keepUnchangedPlan(req, &resp)
	var values map[string]tftypes.Value
	if err := resp.Plan.Raw.As(&values); err != nil {
		t.Fatal(err)
	}
	if values["username"].IsKnown() {
		t.Fatalf("username = %v, want it left unknown", values["username"])
	}
}
