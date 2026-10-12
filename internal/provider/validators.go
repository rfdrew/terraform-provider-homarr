package provider

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// Homarr validates its inputs with zod in JavaScript, so a few checks have to
// count and trim the way JavaScript does rather than the way Go does: a value
// Go accepts but Homarr rewrites is stored differently from what was planned,
// and Terraform rejects the apply as an inconsistent result. Trimming lives in
// the client package as client.JSTrim, because the client needs it too.

// jsLength is String.prototype.length: the number of UTF-16 code units, which
// is what zod's .max() limits.
func jsLength(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// stringCheck adapts a predicate into a string validator. A failing value is
// reported as "Attribute <path> <description>".
type stringCheck struct {
	description string
	check       func(string) bool
}

func (v stringCheck) Description(_ context.Context) string { return v.description }

func (v stringCheck) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v stringCheck) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() || v.check(req.ConfigValue.ValueString()) {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Attribute Value",
		fmt.Sprintf("Attribute %s %s, got: %q", req.Path, v.description, req.ConfigValue.ValueString()),
	)
}

// trimmedValidator rejects leading or trailing whitespace, which Homarr strips
// before storing the value.
func trimmedValidator() validator.String {
	return stringCheck{
		description: "must not start or end with whitespace, which Homarr strips",
		check:       func(s string) bool { return client.JSTrim(s) == s },
	}
}

// notBlankValidator rejects a value that is only whitespace, which Homarr trims
// to nothing and stores as unset; "" is the way to ask for that, and is
// allowed.
func notBlankValidator() validator.String {
	return stringCheck{
		description: `must not consist only of whitespace, which Homarr stores as unset; use "" to clear it`,
		check:       func(s string) bool { return s == "" || client.JSTrim(s) != "" },
	}
}

// jsLengthAtMostValidator limits a value to max UTF-16 code units, the way zod's
// .max() counts. stringvalidator.LengthAtMost counts UTF-8 bytes instead, which
// rejects valid non-ASCII values well before Homarr would.
func jsLengthAtMostValidator(max int) validator.String {
	return stringCheck{
		description: fmt.Sprintf("string length must be at most %d, counted in UTF-16 code units as Homarr does", max),
		check:       func(s string) bool { return jsLength(s) <= max },
	}
}

// jsLengthBetweenValidator is zod's .min(min).max(max), counted in UTF-16 code
// units. stringvalidator.LengthBetween counts UTF-8 bytes, so it lets a short
// non-ASCII value through to a 400 from Homarr and turns away a long one Homarr
// accepts. Its wording is kept, so the errors read the same as before.
func jsLengthBetweenValidator(min, max int) validator.String {
	return stringCheck{
		description: fmt.Sprintf("string length must be between %d and %d, counted in UTF-16 code units as Homarr does", min, max),
		check: func(s string) bool {
			n := jsLength(s)
			return n >= min && n <= max
		},
	}
}
