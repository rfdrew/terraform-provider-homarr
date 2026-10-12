package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func validate(v validator.String, value string) bool {
	resp := validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{
		Path:        path.Root("attr"),
		ConfigValue: types.StringValue(value),
	}, &resp)
	return !resp.Diagnostics.HasError()
}

func TestJSLengthCountsUTF16Units(t *testing.T) {
	for in, want := range map[string]int{
		"abc": 3,
		"абв": 3, // 6 UTF-8 bytes
		"😀":   2, // a surrogate pair
		"":    0,
	} {
		if got := jsLength(in); got != want {
			t.Errorf("jsLength(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTrimmedValidator(t *testing.T) {
	for value, want := range map[string]bool{
		"":        true,
		"name":    true,
		"a b":     true,
		" name":   false,
		"name\n":  false,
		"\ufeffx": false,
	} {
		if got := validate(trimmedValidator(), value); got != want {
			t.Errorf("trimmedValidator(%q) passes = %v, want %v", value, got, want)
		}
	}
}

func TestNotBlankValidatorMatchesWhatHomarrTrims(t *testing.T) {
	for value, want := range map[string]bool{
		"":             true,
		"Infra":        true,
		"  padded  ":   true,
		" ":            false,
		"\t\n":         false,
		"\u00a0\u3000": false,
		"\ufeff":       false,
		"\v":           false,
	} {
		if got := validate(notBlankValidator(), value); got != want {
			t.Errorf("notBlankValidator(%q) passes = %v, want %v", value, got, want)
		}
	}
}

func TestJSLengthAtMostValidatorCountsLikeHomarr(t *testing.T) {
	v := jsLengthAtMostValidator(16384)
	// 9000 Cyrillic letters are 18000 UTF-8 bytes but 9000 UTF-16 units, well
	// within Homarr's limit; LengthAtMost would have rejected them.
	if !validate(v, strings.Repeat("ж", 9000)) {
		t.Error("rejected 9000 Cyrillic characters")
	}
	if validate(v, strings.Repeat("a", 16385)) {
		t.Error("accepted 16385 characters")
	}
	if validate(v, strings.Repeat("😀", 8193)) {
		t.Error("accepted 8193 emoji, which are 16386 UTF-16 units")
	}
}
