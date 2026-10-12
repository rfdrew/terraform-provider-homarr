package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Resource identity is the small object Terraform 1.12 and later store next to
// a resource's state to say which remote object it is, and accept in an import
// block's `identity` argument instead of an import id. Every resource here that
// has one is identified by a single Homarr id, which never changes while the
// object exists and which Read always knows, because it is what Read looks up.
//
// Two resources deliberately have none:
//
//   - homarr_board. Importing a board needs its column count, which Homarr
//     never reports, so an identity of the id alone could not import one: the
//     column count would land in state as null and the next plan would replace
//     the board. Adding column_count to the identity would be worse. It is
//     configuration rather than identification, Read could only copy it from
//     state instead of reading it from Homarr, and it can change in the Homarr
//     UI without Terraform noticing — so two identities could name one board,
//     and an identity could claim a column count the board no longer has.
//   - homarr_server_board_settings. It is a singleton whose only key is the
//     constant "board", so every Homarr instance would share one identity.
//
// The framework requires an identity after every successful create, read and
// update of a resource that declares one, whether or not Terraform supports
// identities. Older Terraform neither sends nor stores it and is otherwise
// unaffected.

// idIdentitySchema is the identity of a resource identified by one Homarr id,
// held in the identity attribute of the given name.
func idIdentitySchema(attribute, description string) identityschema.Schema {
	return identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			attribute: identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       description,
			},
		},
	}
}

// setIdentity records id as the identity attribute of the given name. identity
// is nil only when a method is called directly rather than through the
// framework, and an empty id has nothing to record.
//
// Read calls it with the id from state before looking the object up. A refresh
// that finds the object gone drops it from state without an error, and the
// framework still requires an identity then whenever state had none yet —
// state written by provider 0.2 or by Terraform before 1.12 — failing the
// refresh with "Missing Resource Identity After Read" otherwise.
func setIdentity(ctx context.Context, identity *tfsdk.ResourceIdentity, attribute, id string, diags *diag.Diagnostics) {
	if identity == nil || id == "" {
		return
	}
	diags.Append(identity.SetAttribute(ctx, path.Root(attribute), id)...)
}

// importedID returns what an import names the resource by: the import id, in
// whatever form the resource accepts, or — for an import block that used
// `identity` — the identity attribute of the given name, which always holds a
// plain Homarr id. byIdentity tells the two apart.
func importedID(ctx context.Context, req resource.ImportStateRequest, attribute string, diags *diag.Diagnostics) (id string, byIdentity bool) {
	if req.ID != "" || req.Identity == nil {
		return req.ID, false
	}
	var value types.String
	diags.Append(req.Identity.GetAttribute(ctx, path.Root(attribute), &value)...)
	if diags.HasError() {
		return "", true
	}
	if value.ValueString() == "" {
		diags.AddError(
			"Missing import identity",
			"The import block's identity must set "+attribute+" to the Homarr id of the object to import.",
		)
		return "", true
	}
	return value.ValueString(), true
}
