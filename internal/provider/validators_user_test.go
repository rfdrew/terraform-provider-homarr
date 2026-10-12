package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Homarr validates email with zod's unicodeEmail pattern,
// /^[^\s@"]{1,64}@[^\s@]{1,255}$/u, where \s is JavaScript's Unicode-aware
// class and the bounds count code points.
func TestEmailValidatorMatchesHomarr(t *testing.T) {
	for value, want := range map[string]bool{
		"viewer@example.com":                    true,
		"a@b":                                   true, // Homarr checks the shape only
		"first.last+tag@sub.example":            true,
		"j\u00fcrgen@b\u00fccher.de":            true,
		"\U0001f600@x":                          true, // one code point, though two UTF-16 units
		strings.Repeat("a", 64) + "@x":          true,
		"a@" + strings.Repeat("b", 255):         true,
		strings.Repeat("\u0436", 64) + "@x":     true, // 128 UTF-8 bytes, 64 code points
		"a\u0085b@c":                            true, // NEL is whitespace to Go, not to JavaScript
		"a\u200bb@c":                            true, // a zero-width space is not a space separator
		"":                                      false,
		"viewer":                                false,
		"@example.com":                          false,
		"viewer@":                               false,
		"a@@b":                                  false,
		"a@b@c":                                 false,
		`"quoted"@example.com`:                  false,
		"a b@c":                                 false,
		"a@b c":                                 false,
		"a\vb@c":                                false, // \v, which Go's \s leaves out
		"a\u00a0b@c":                            false, // no-break space
		"a@b\u2028":                             false, // line separator
		"\ufeffa@b":                             false, // byte order mark
		"a@b\u3000c":                            false, // ideographic space
		"a@\u2003":                              false, // em space
		strings.Repeat("a", 65) + "@x":          false,
		"a@" + strings.Repeat("b", 256):         false,
		strings.Repeat("\U0001f600", 65) + "@x": false,
		"tab\t@example.com":                     false,
		"viewer@example.com\n":                  false,
		strings.Repeat("a", 64) + "@" + "x\"":   true, // a quote is only barred before the @
	} {
		if got := validate(emailValidator(), value); got != want {
			t.Errorf("emailValidator(%q) passes = %v, want %v", value, got, want)
		}
	}
}

func TestUserEmailUsesTheEmailValidator(t *testing.T) {
	requireErrorContaining(t, userAttributeDiagnostics(t, "email", "not-an-email"), "must be an email address")
	if diags := userAttributeDiagnostics(t, "email", "viewer@example.com"); diags.HasError() {
		t.Fatalf("a valid email was rejected: %v", diags)
	}
}

// --- header ------------------------------------------------------------------

// headerItem builds one configured header item; "" leaves a field null.
func headerItem(builtin, boardID string) attr.Value {
	b, board := types.StringNull(), types.StringNull()
	if builtin != "" {
		b = types.StringValue(builtin)
	}
	if boardID != "" {
		board = types.StringValue(boardID)
	}
	return types.ObjectValueMust(headerItemAttrTypes, map[string]attr.Value{"builtin": b, "board_id": board})
}

func headerZone(items ...attr.Value) types.List {
	return types.ListValueMust(headerItemType, items)
}

// headerConfig builds a configured header from its zones; a missing zone is
// null, as when the configuration leaves it out.
func headerConfig(zones map[string]attr.Value) types.Object {
	values := map[string]attr.Value{
		"visible":        types.BoolValue(true),
		"search_display": types.StringValue("input"),
		"logo_display":   types.StringValue("logoAndText"),
		"left":           types.ListNull(headerItemType),
		"center":         types.ListNull(headerItemType),
		"right":          types.ListNull(headerItemType),
	}
	for name, zone := range zones {
		values[name] = zone
	}
	return types.ObjectValueMust(headerAttrTypes, values)
}

func validateHeader(v types.Object) diag.Diagnostics {
	resp := validator.ObjectResponse{}
	headerItemsValidator{}.ValidateObject(context.Background(), validator.ObjectRequest{
		Path:        path.Root("header"),
		ConfigValue: v,
	}, &resp)
	return resp.Diagnostics
}

func TestHeaderValidatorAcceptsAValidLayout(t *testing.T) {
	for name, header := range map[string]types.Object{
		"Homarr's default": headerConfig(map[string]attr.Value{
			"left":   headerZone(headerItem("logo", "")),
			"center": headerZone(headerItem("search", "")),
			"right":  headerZone(headerItem("boardEdit", ""), headerItem("boardSettings", ""), headerItem("user", "")),
		}),
		"only the account menu": headerConfig(map[string]attr.Value{"center": headerZone(headerItem("user", ""))}),
		"two different boards": headerConfig(map[string]attr.Value{
			"left":  headerZone(headerItem("", "b1")),
			"right": headerZone(headerItem("", "b2"), headerItem("user", "")),
		}),
		"null":    types.ObjectNull(headerAttrTypes),
		"unknown": types.ObjectUnknown(headerAttrTypes),
	} {
		if diags := validateHeader(header); diags.HasError() {
			t.Errorf("%s: unexpected errors: %v", name, diags)
		}
	}
}

func TestHeaderValidatorRejectsDuplicatesAcrossZones(t *testing.T) {
	for name, header := range map[string]types.Object{
		"builtin in two zones": headerConfig(map[string]attr.Value{
			"left":  headerZone(headerItem("search", "")),
			"right": headerZone(headerItem("search", ""), headerItem("user", "")),
		}),
		"board twice in one zone": headerConfig(map[string]attr.Value{
			"right": headerZone(headerItem("", "b1"), headerItem("", "b1"), headerItem("user", "")),
		}),
	} {
		t.Run(name, func(t *testing.T) {
			requireErrorContaining(t, validateHeader(header), "Duplicate header item")
		})
	}

	// A builtin and a board with the same id are different items to Homarr.
	sameID := headerConfig(map[string]attr.Value{"right": headerZone(headerItem("", "logo"), headerItem("logo", ""), headerItem("user", ""))})
	if diags := validateHeader(sameID); diags.HasError() {
		t.Errorf("a board with a builtin's name was taken for that builtin: %v", diags)
	}
}

func TestHeaderValidatorRequiresTheAccountMenu(t *testing.T) {
	for name, header := range map[string]types.Object{
		"no user":     headerConfig(map[string]attr.Value{"left": headerZone(headerItem("logo", ""))}),
		"no items":    headerConfig(nil),
		"empty zones": headerConfig(map[string]attr.Value{"left": headerZone(), "center": headerZone(), "right": headerZone()}),
	} {
		diags := validateHeader(header)
		if !diags.HasError() {
			t.Errorf("%s: accepted a header without the user builtin", name)
			continue
		}
		requireErrorContaining(t, diags, `builtin = "user"`)
	}
}

// A zone or item that is not known yet may still supply the account menu, or
// clash with another item, so the validator holds back.
func TestHeaderValidatorWaitsForUnknownValues(t *testing.T) {
	for name, header := range map[string]types.Object{
		"unknown zone": headerConfig(map[string]attr.Value{
			"left":  headerZone(headerItem("logo", "")),
			"right": types.ListUnknown(headerItemType),
		}),
		"unknown item": headerConfig(map[string]attr.Value{
			"right": headerZone(types.ObjectUnknown(headerItemAttrTypes)),
		}),
		"unknown board id": headerConfig(map[string]attr.Value{
			"right": headerZone(types.ObjectValueMust(headerItemAttrTypes, map[string]attr.Value{
				"builtin":  types.StringNull(),
				"board_id": types.StringUnknown(),
			})),
		}),
	} {
		if diags := validateHeader(header); diags.HasError() {
			t.Errorf("%s: judged before the value was known: %v", name, diags)
		}
	}
}

// headerZoneSchema returns the schema of one zone of homarr_user's header.
func headerZoneSchema(t *testing.T, zone string) schema.ListNestedAttribute {
	t.Helper()
	header, ok := resourceSchema(t, &userResource{}).Attributes["header"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("homarr_user.header is not a single nested attribute")
	}
	attribute, ok := header.Attributes[zone].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("homarr_user.header.%s is not a list nested attribute", zone)
	}
	return attribute
}

func TestHeaderZonesHoldAtMostAHundredItems(t *testing.T) {
	zone := headerZoneSchema(t, "right")
	items := make([]attr.Value, 0, 101)
	for i := 0; i < 101; i++ {
		items = append(items, headerItem("", "b"+strings.Repeat("x", i)))
	}

	var diags diag.Diagnostics
	for _, v := range zone.Validators {
		resp := validator.ListResponse{}
		v.ValidateList(context.Background(), validator.ListRequest{
			Path:        path.Root("header").AtName("right"),
			ConfigValue: headerZone(items...),
		}, &resp)
		diags.Append(resp.Diagnostics...)
	}
	requireErrorContaining(t, diags, "100")
}

// Every header item names exactly one of a builtin and a board. The check reads
// the item's other field from the configuration, so a whole configuration is
// built here.
func TestHeaderItemsNameExactlyOneOfBuiltinAndBoard(t *testing.T) {
	r := &userResource{}
	s := resourceSchema(t, r)
	builtin, ok := headerZoneSchema(t, "right").NestedObject.Attributes["builtin"].(schema.StringAttribute)
	if !ok {
		t.Fatal("header item builtin is not a string attribute")
	}

	for name, tc := range map[string]struct {
		item    attr.Value
		wantErr bool
	}{
		"builtin only": {headerItem("user", ""), false},
		"board only":   {headerItem("", "b1"), false},
		"both":         {headerItem("user", "b1"), true},
		"neither":      {headerItem("", ""), true},
	} {
		model := userTestModel()
		model.Header = headerConfig(map[string]attr.Value{"right": headerZone(tc.item)})
		config := tfsdk.Config{Schema: s, Raw: stateOf(t, s, &model).Raw}

		itemPath := path.Root("header").AtName("right").AtListIndex(0).AtName("builtin")
		var value types.String
		requireNoErrors(t, config.GetAttribute(context.Background(), itemPath, &value))

		var diags diag.Diagnostics
		for _, v := range builtin.Validators {
			resp := validator.StringResponse{}
			v.ValidateString(context.Background(), validator.StringRequest{
				Config:         config,
				Path:           itemPath,
				PathExpression: itemPath.Expression(),
				ConfigValue:    value,
			}, &resp)
			diags.Append(resp.Diagnostics...)
		}
		if diags.HasError() != tc.wantErr {
			t.Errorf("%s: errors = %v, want errors %v", name, diags, tc.wantErr)
		}
	}
}

// --- group_ids -----------------------------------------------------------------

func TestGroupIDsMustBeUniqueAndNonEmpty(t *testing.T) {
	attribute, ok := resourceSchema(t, &userResource{}).Attributes["group_ids"].(schema.ListAttribute)
	if !ok {
		t.Fatal("homarr_user.group_ids is not a list attribute")
	}
	run := func(values ...string) diag.Diagnostics {
		elements := make([]attr.Value, 0, len(values))
		for _, v := range values {
			elements = append(elements, types.StringValue(v))
		}
		var diags diag.Diagnostics
		for _, v := range attribute.Validators {
			resp := validator.ListResponse{}
			v.ValidateList(context.Background(), validator.ListRequest{
				Path:        path.Root("group_ids"),
				ConfigValue: types.ListValueMust(types.StringType, elements),
			}, &resp)
			diags.Append(resp.Diagnostics...)
		}
		return diags
	}

	if diags := run("g1", "g2"); diags.HasError() {
		t.Errorf("distinct group ids rejected: %v", diags)
	}
	if !run("g1", "g1").HasError() {
		t.Error("a duplicate group id was accepted; Homarr fails the create after making the user")
	}
	if !run("g1", "").HasError() {
		t.Error("an empty group id was accepted")
	}
}
