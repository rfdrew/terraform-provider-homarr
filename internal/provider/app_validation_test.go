package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// Homarr's appManageSchema trims every string an app carries and stores "" as
// null for description, href and ping_url. homarr_app writes Homarr's answer
// into state, so these tests hold the schema to one rule: a value that passes
// plan-time validation is one Homarr stores unchanged.

// appStringAttributes are the configurable homarr_app attributes, each with a
// value Homarr accepts as it is. Inner whitespace survives Homarr's trim, so
// the names and descriptions carry some.
var appStringAttributes = map[string]string{
	"name":        "Home  Assistant",
	"description": "Dashboards for the homelab",
	"icon_url":    testIconURL,
	"href":        "https://grafana.example.com",
	"ping_url":    "https://grafana.example.com/api/health",
}

// appOptionalAttributes are the attributes appManageSchema turns from "" into
// null.
var appOptionalAttributes = map[string]bool{"description": true, "href": true, "ping_url": true}

// validateAppAttribute runs every validator the homarr_app schema declares on
// attr against value, as Terraform does at plan time.
func validateAppAttribute(t *testing.T, attr string, value types.String) diag.Diagnostics {
	t.Helper()
	a, ok := resourceSchema(t, &appResource{}).Attributes[attr].(schema.StringAttribute)
	if !ok {
		t.Fatalf("homarr_app has no string attribute %q", attr)
	}
	var diags diag.Diagnostics
	for _, v := range a.Validators {
		var resp validator.StringResponse
		v.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root(attr),
			ConfigValue: value,
		}, &resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

// homarrStoredApp is what appManageSchema makes of value before it is stored:
// String.prototype.trim, then null in place of "" for the optional attributes.
func homarrStoredApp(attr, value string) *string {
	trimmed := client.JSTrim(value)
	if trimmed == "" && appOptionalAttributes[attr] {
		return nil
	}
	return &trimmed
}

func TestAppAttributesAcceptValuesHomarrKeeps(t *testing.T) {
	for attr, value := range appStringAttributes {
		if diags := validateAppAttribute(t, attr, types.StringValue(value)); diags.HasError() {
			t.Errorf("%s = %q rejected: %v", attr, value, diags)
		}
	}
}

func TestAppAttributesRejectWhitespaceHomarrTrims(t *testing.T) {
	for attr, value := range appStringAttributes {
		for _, padded := range []string{
			" " + value,
			value + " ",
			value + "\n",
			"\t" + value,
			"\u00a0" + value, // a no-break space, trimmed by JavaScript
			value + "\ufeff", // a byte-order mark, trimmed by JavaScript but not by strings.TrimSpace
			" ",
		} {
			diags := validateAppAttribute(t, attr, types.StringValue(padded))
			if !diags.HasError() {
				t.Errorf("%s = %q accepted, but Homarr would store %q", attr, padded, client.JSTrim(padded))
				continue
			}
			requireErrorContaining(t, diags, "must not start or end with whitespace")
		}
	}
}

func TestAppAttributesRejectEmptyString(t *testing.T) {
	for attr := range appStringAttributes {
		if diags := validateAppAttribute(t, attr, types.StringValue("")); !diags.HasError() {
			t.Errorf("%s = \"\" accepted, but Homarr stores it as null or refuses it", attr)
		}
	}
}

func TestAppOptionalAttributesAcceptNullAndUnknown(t *testing.T) {
	for attr := range appOptionalAttributes {
		for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
			if diags := validateAppAttribute(t, attr, value); diags.HasError() {
				t.Errorf("%s = %v rejected: %v", attr, value, diags)
			}
		}
	}
}

// TestAppAcceptedValuesAreStoredUnchanged checks the rule itself: across
// a spread of awkward values, whatever the schema lets through is what Homarr
// would store, so the apply cannot come back inconsistent.
func TestAppAcceptedValuesAreStoredUnchanged(t *testing.T) {
	samples := []string{
		"", " ", "\t\n", "\u3000", "\u2028x", "x\u2029", "\u0085x\u0085",
		"plain", " lead", "trail ", "in ner", "Ж", "https://example.com/ ",
	}
	for attr := range appStringAttributes {
		for _, value := range samples {
			if validateAppAttribute(t, attr, types.StringValue(value)).HasError() {
				continue
			}
			stored := homarrStoredApp(attr, value)
			if stored == nil || *stored != value {
				t.Errorf("%s = %q accepted, but Homarr would store %v", attr, value, stringValueOrNull(stored))
			}
		}
	}
}

func TestAppLengthLimitsCountLikeHomarr(t *testing.T) {
	for _, tc := range []struct {
		attr  string
		value string
		ok    bool
	}{
		{"name", strings.Repeat("a", 64), true},
		{"name", strings.Repeat("a", 65), false},
		// 64 Cyrillic letters are 128 UTF-8 bytes but 64 UTF-16 units, which
		// Homarr accepts; stringvalidator.LengthBetween would not.
		{"name", strings.Repeat("ж", 64), true},
		{"name", strings.Repeat("😀", 32), true},
		{"name", strings.Repeat("😀", 33), false},
		{"description", strings.Repeat("ж", 512), true},
		{"description", strings.Repeat("a", 513), false},
		{"description", strings.Repeat("😀", 257), false},
	} {
		diags := validateAppAttribute(t, tc.attr, types.StringValue(tc.value))
		if got := !diags.HasError(); got != tc.ok {
			t.Errorf("%s of %d UTF-16 units passes = %v, want %v: %v", tc.attr, jsLength(tc.value), got, tc.ok, diags)
		}
	}
}

func TestAppEmptyNameKeepsItsMessage(t *testing.T) {
	// TestAccAppResource_invalidName matches this wording against a real apply.
	diags := validateAppAttribute(t, "name", types.StringValue(""))
	requireErrorContaining(t, diags, "Attribute name string length must be between 1 and 64")
}
