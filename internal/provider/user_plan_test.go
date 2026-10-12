package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// These tests run plans and validation through the provider's protocol server,
// the way Terraform calls it, for homarr_user behaviour that only shows up with
// the framework's whole planning sequence: computed nested values, plan
// modifiers and configuration validators together. No Homarr is involved; the
// unconfigured provider leaves the resource without a client.

func TestUserSchemasAreValid(t *testing.T) {
	for _, r := range []resource.Resource{&userResource{}, &userPreferencesResource{}} {
		if diags := resourceSchema(t, r).ValidateImplementation(context.Background()); diags.HasError() {
			t.Errorf("%T schema is invalid: %v", r, diags)
		}
	}
}

func protocolServer(t *testing.T) tfprotov6.ProviderServer {
	t.Helper()
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func dynamicValue(t *testing.T, model any) *tfprotov6.DynamicValue {
	t.Helper()
	s := resourceSchema(t, &userResource{})
	raw := stateOf(t, s, model).Raw
	dv, err := tfprotov6.NewDynamicValue(s.Type().TerraformType(context.Background()), raw)
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

// userConfigOf turns a homarr_user state into the configuration that would
// produce it: computed-only attributes become null, and so do the optional
// computed ones that the configuration leaves to Homarr.
func userConfigOf(m userModel) userModel {
	m.ID, m.Provider, m.Image = types.StringNull(), types.StringNull(), types.StringNull()
	m.ColorScheme, m.ByteUnitSystem = types.StringNull(), types.StringNull()
	m.FirstDayOfWeek = types.Int64Null()
	m.PingIconsEnabled, m.EnableRightClickOnWidgets = types.BoolNull(), types.BoolNull()
	m.OpenSearchInNewTab, m.DdgBangs = types.BoolNull(), types.BoolNull()
	m.Header = types.ObjectNull(headerAttrTypes)
	return m
}

// planUser plans prior towards config. Like Terraform, it proposes the
// configuration with the prior value of every optional computed attribute the
// configuration leaves null — except the nested `header`, for which Terraform
// proposes null, the value that makes the framework see a change where there is
// none (see keepUnchangedPlan).
func planUser(t *testing.T, prior, config userModel) (userModel, *tfprotov6.PlanResourceChangeResponse) {
	t.Helper()
	proposed := config
	proposed.ID, proposed.Provider = prior.ID, prior.Provider
	keep := func(configured, previous types.String) types.String {
		if configured.IsNull() {
			return previous
		}
		return configured
	}
	proposed.Image = keep(config.Image, prior.Image)
	proposed.ColorScheme = keep(config.ColorScheme, prior.ColorScheme)
	proposed.ByteUnitSystem = keep(config.ByteUnitSystem, prior.ByteUnitSystem)
	if config.FirstDayOfWeek.IsNull() {
		proposed.FirstDayOfWeek = prior.FirstDayOfWeek
	}
	for _, pair := range []struct{ configured, previous, target *types.Bool }{
		{&config.PingIconsEnabled, &prior.PingIconsEnabled, &proposed.PingIconsEnabled},
		{&config.EnableRightClickOnWidgets, &prior.EnableRightClickOnWidgets, &proposed.EnableRightClickOnWidgets},
		{&config.OpenSearchInNewTab, &prior.OpenSearchInNewTab, &proposed.OpenSearchInNewTab},
		{&config.DdgBangs, &prior.DdgBangs, &proposed.DdgBangs},
	} {
		if pair.configured.IsNull() {
			*pair.target = *pair.previous
		}
	}
	resp, err := protocolServer(t).PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "homarr_user",
		PriorState:       dynamicValue(t, &prior),
		ProposedNewState: dynamicValue(t, &proposed),
		Config:           dynamicValue(t, &config),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan failed: %s: %s", d.Summary, d.Detail)
		}
	}

	s := resourceSchema(t, &userResource{})
	raw, err := resp.PlannedState.Unmarshal(s.Type().TerraformType(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	var planned userModel
	requireNoErrors(t, tfsdk.Plan{Schema: s, Raw: raw}.Get(context.Background(), &planned))
	return planned, resp
}

func readUserState() userModel {
	m := userTestModel()
	m.Header = headerValueFromAPI(homarrDefaultHeader(), types.ObjectNull(headerAttrTypes))
	return m
}

func TestUserPlanWithNothingChangedIsEmpty(t *testing.T) {
	prior := readUserState()
	planned, resp := planUser(t, prior, userConfigOf(prior))

	if len(resp.RequiresReplace) != 0 {
		t.Fatalf("requires replace: %v", resp.RequiresReplace)
	}
	if !planned.Header.Equal(prior.Header) || !planned.ColorScheme.Equal(prior.ColorScheme) {
		t.Fatalf("planned header = %v, color_scheme = %v; want the prior values", planned.Header, planned.ColorScheme)
	}
}

// The header keeps its prior state while unconfigured even when something else
// changes, rather than turning into "(known after apply)".
func TestUserPlanRenamesInPlaceAndKeepsTheHeader(t *testing.T) {
	prior := readUserState()
	config := userConfigOf(prior)
	config.Username = types.StringValue("bob")
	config.Email = types.StringValue("bob@example.com")

	planned, resp := planUser(t, prior, config)
	if len(resp.RequiresReplace) != 0 {
		t.Fatalf("a rename or new email requires replacement: %v", resp.RequiresReplace)
	}
	if planned.Username.ValueString() != "bob" {
		t.Fatalf("planned username = %v", planned.Username)
	}
	if !planned.Header.Equal(prior.Header) {
		t.Fatalf("planned header = %v, want the prior layout kept", planned.Header)
	}
}

func TestUserPlanTakesAConfiguredHeaderAsWritten(t *testing.T) {
	prior := readUserState()
	config := userConfigOf(prior)
	config.Header = headerConfig(map[string]attr.Value{"right": headerZone(headerItem("user", ""))})

	planned, _ := planUser(t, prior, config)
	if !planned.Header.Equal(config.Header) {
		t.Fatalf("planned header = %v, want the configuration, with no zone filled in", planned.Header)
	}
}

func TestUserPlanReplacesWhenTheEmailIsRemoved(t *testing.T) {
	prior := readUserState()
	prior.Email = types.StringValue("alice@example.com")
	config := userConfigOf(prior)
	config.Email = types.StringNull()

	_, resp := planUser(t, prior, config)
	if len(resp.RequiresReplace) != 1 || !strings.Contains(resp.RequiresReplace[0].String(), "email") {
		t.Fatalf("requires replace = %v, want email", resp.RequiresReplace)
	}
}

func validateUserConfig(t *testing.T, config userModel) []*tfprotov6.Diagnostic {
	t.Helper()
	resp, err := protocolServer(t).ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName:           "homarr_user",
		Config:             dynamicValue(t, &config),
		ClientCapabilities: &tfprotov6.ValidateResourceConfigClientCapabilities{WriteOnlyAttributesAllowed: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Diagnostics
}

func anyError(diags []*tfprotov6.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}

func diagnosticsMention(diags []*tfprotov6.Diagnostic, want string) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError && (strings.Contains(d.Summary, want) || strings.Contains(d.Detail, want)) {
			return true
		}
	}
	return false
}

func TestUserConfigNeedsExactlyOnePassword(t *testing.T) {
	base := userConfigOf(readUserState())

	withPassword := base
	if diags := validateUserConfig(t, withPassword); anyError(diags) {
		t.Errorf("password alone rejected: %v", diags)
	}

	writeOnly := base
	writeOnly.Password = types.StringNull()
	writeOnly.PasswordWO = types.StringValue("Write-Only-Pass-1")
	writeOnly.PasswordWOVersion = types.Int64Value(1)
	if diags := validateUserConfig(t, writeOnly); anyError(diags) {
		t.Errorf("password_wo alone rejected: %v", diags)
	}

	neither := base
	neither.Password = types.StringNull()
	if diags := validateUserConfig(t, neither); !diagnosticsMention(diags, "password_wo") {
		t.Errorf("no password accepted: %v", diags)
	}

	both := base
	both.PasswordWO = types.StringValue("Write-Only-Pass-1")
	if diags := validateUserConfig(t, both); !diagnosticsMention(diags, "password_wo") {
		t.Errorf("both passwords accepted: %v", diags)
	}

	versionOnly := base
	versionOnly.PasswordWOVersion = types.Int64Value(1)
	if diags := validateUserConfig(t, versionOnly); !diagnosticsMention(diags, "password_wo") {
		t.Errorf("password_wo_version without password_wo accepted: %v", diags)
	}
}

func TestUserConfigValidatesTheHeader(t *testing.T) {
	config := userConfigOf(readUserState())
	config.Header = headerConfig(map[string]attr.Value{
		"left":  headerZone(headerItem("logo", "")),
		"right": headerZone(headerItem("logo", "")),
	})
	diags := validateUserConfig(t, config)
	for _, want := range []string{"Duplicate header item", `builtin = "user"`} {
		if !diagnosticsMention(diags, want) {
			t.Errorf("no error mentioning %q in %v", want, diags)
		}
	}

	// The framework only validates the items once the header as a whole passed.
	config.Header = headerConfig(map[string]attr.Value{
		"right": headerZone(headerItem("user", ""), headerItem("logo", "b1")),
	})
	if diags := validateUserConfig(t, config); !diagnosticsMention(diags, "board_id") {
		t.Errorf("an item naming both a builtin and a board was accepted: %v", diags)
	}
}
