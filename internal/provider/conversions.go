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

// int64PointerOrNil converts a Terraform integer into a pointer, so that unset
// attributes can be omitted from a partial patch body.
func int64PointerOrNil(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

// stringValueOrNullIfEmpty maps an API string to Terraform, treating "" as
// absent. Homarr omits some preference fields on releases that predate them,
// which decodes to the zero value rather than a meaningful setting.
func stringValueOrNullIfEmpty(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
