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
)

// Homarr trims and lowercases usernames before storing them, and username is
// Required, so a value Homarr would rewrite has to be refused at plan time:
// otherwise the apply fails as an inconsistent result and an import plans a
// replacement forever. These tests run the validators the schema actually
// declares, so they also catch the attribute losing one.

func usernameDiagnostics(t *testing.T, value string) diag.Diagnostics {
	t.Helper()
	return userAttributeDiagnostics(t, "username", value)
}

// userAttributeDiagnostics runs the validators homarr_user declares on name.
func userAttributeDiagnostics(t *testing.T, name, value string) diag.Diagnostics {
	t.Helper()
	attr, ok := resourceSchema(t, &userResource{}).Attributes[name].(schema.StringAttribute)
	if !ok {
		t.Fatalf("homarr_user.%s is not a string attribute", name)
	}
	var diags diag.Diagnostics
	for _, v := range attr.Validators {
		resp := validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root(name),
			ConfigValue: types.StringValue(value),
		}, &resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

func TestUsernameAcceptsWhatHomarrStoresUnchanged(t *testing.T) {
	for _, value := range []string{
		"tfuser",
		"tf-user_1",
		"tf user", // Homarr trims the ends only, so inner whitespace survives
		"ärger",
	} {
		if diags := usernameDiagnostics(t, value); diags.HasError() {
			t.Errorf("username %q rejected: %v", value, diags)
		}
	}
}

func TestUsernameRejectsWhatHomarrWouldRewrite(t *testing.T) {
	for _, value := range []string{
		"TfUser",
		" tfuser",
		"tfuser\n",
		"Ärger",
		"\ufefftfuser", // trimmed by JavaScript but not by strings.TrimSpace
	} {
		requireErrorContaining(t, usernameDiagnostics(t, value), "must be lowercase with no leading or trailing whitespace")
	}
}

func TestUsernameLengthIsCountedLikeHomarr(t *testing.T) {
	for value, want := range map[string]bool{
		"abc":                    true,
		"ab":                     false,
		"ää":                     false, // 4 UTF-8 bytes but only 2 characters to Homarr
		strings.Repeat("ж", 200): true,  // 400 UTF-8 bytes but 200 characters to Homarr
		strings.Repeat("a", 255): true,
		strings.Repeat("a", 256): false,
		strings.Repeat("😀", 127): true,  // 254 UTF-16 units
		strings.Repeat("😀", 128): false, // 256 UTF-16 units
	} {
		diags := usernameDiagnostics(t, value)
		if got := !diags.HasError(); got != want {
			t.Errorf("username of %d bytes passes = %v, want %v: %v", len(value), got, want, diags)
		}
		if !want {
			requireErrorContaining(t, diags, "between 3 and 255")
		}
	}
}

// userPasswordSchema is z.string().min(8).max(255), counted in UTF-16 code units
// like every zod string length.
func TestPasswordLengthIsCountedLikeHomarr(t *testing.T) {
	for value, want := range map[string]bool{
		"TfProvider!23":          true,
		"секретик":               true,  // 8 letters, 16 bytes
		strings.Repeat("ж", 255): true,  // 510 bytes, which a byte count refused
		"ключ":                   false, // 8 bytes but 4 letters: Homarr answers 400
		strings.Repeat("ж", 256): false,
		strings.Repeat("a", 7):   false,
	} {
		if got := !userAttributeDiagnostics(t, "password", value).HasError(); got != want {
			t.Errorf("password of %d characters accepted = %v, want %v", jsLength(value), got, want)
		}
	}
}
