package provider

import (
	"iter"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
)

// List resources answer `terraform query` (Terraform 1.14 and later): a list
// block in a .tfquery.hcl file names a resource type, and the provider reports
// every object of that type it can find, each with its resource identity, a
// display name and — when the block sets include_resource — the attributes
// Terraform would hold after importing it. `terraform query
// -generate-config-out` turns the results into import blocks and resource
// configuration.
//
// Only resources with an identity can be listed, which rules out homarr_board
// (see resource_identity.go). homarr_board_copy has one but is not listed
// either: Homarr keeps no record of which boards are copies, so its list would
// offer every board — those managed as homarr_board included — as a copy to
// import, with a source_board_id nobody can fill in.

// listLimitReached reports whether a list has already produced as many results
// as Terraform asked for. A limit of 0 sets none.
func listLimitReached(req list.ListRequest, produced int) bool {
	return req.Limit > 0 && int64(produced) >= req.Limit
}

// listError is a result stream that carries nothing but one error.
func listError(summary, detail string) iter.Seq[list.ListResult] {
	var diags diag.Diagnostics
	diags.AddError(summary, detail)
	return list.ListResultsStreamDiagnostics(diags)
}

// unconfiguredListError is the error for a list run before the provider was
// configured, which Terraform never does.
func unconfiguredListError() iter.Seq[list.ListResult] {
	return listError(
		"Unconfigured Homarr provider",
		"The list ran without a configured provider. This is a bug in the provider.",
	)
}
