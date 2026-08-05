package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringPointerOrNil converts a Terraform string into a pointer suitable for a
// JSON body: null and unknown become nil, which serialises as an explicit null.
func stringPointerOrNil(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// stringValueOrNull converts a pointer from an API response back into a
// Terraform string.
func stringValueOrNull(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// boolPointerOrNil converts a Terraform bool into a pointer, so that unset
// attributes can be omitted from a partial patch body.
func boolPointerOrNil(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// float64PointerOrNil converts a Terraform number into a pointer, so that unset
// attributes can be omitted from a partial patch body.
func float64PointerOrNil(v types.Float64) *float64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	f := v.ValueFloat64()
	return &f
}
