package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// This file holds the parts of homarr_user that deal with the account rather
// than its preferences: the password, the email and groups a change of which
// forces a new user, the auth provider, the API key's own account, and import.

// emailCleared plans a replacement when the email is removed from a user that
// has one; see emailRemoved.
func emailCleared(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = emailRemoved(req.PlanValue, req.StateValue)
}

// emailRemoved reports whether planned drops the email prior holds.
// PUT /api/users/profile writes an email only when the new value is non-empty,
// so removing one can never be applied in place. "" in state is no email
// either; fromAPI reads it as null.
func emailRemoved(planned, prior types.String) bool {
	return planned.IsNull() && !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() != ""
}

// groupMembershipChanged plans a replacement when the group membership
// changes; see groupsDiffer.
func groupMembershipChanged(_ context.Context, req planmodifier.ListRequest, resp *listplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = groupsDiffer(req.PlanValue, req.StateValue)
}

// groupsDiffer reports whether the planned groups differ from those in state
// as a set. Memberships cannot change after creation, but the order of the
// list means nothing to Homarr, and a null list and an empty one both mean no
// groups; such changes only rewrite state. A list that is not fully known yet
// may still differ, so it counts as a change.
func groupsDiffer(planned, prior types.List) bool {
	plannedSet, plannedKnown := stringSet(planned)
	priorSet, priorKnown := stringSet(prior)
	return !plannedKnown || !priorKnown || !maps.Equal(plannedSet, priorSet)
}

// replacesUser reports whether a plan for an existing user replaces it, which
// deletes the user. It repeats the decisions of the attribute plan modifiers,
// whose verdict a resource's ModifyPlan does not get to see.
func replacesUser(plan, state userModel) bool {
	return emailRemoved(plan.Email, state.Email) || groupsDiffer(plan.GroupIDs, state.GroupIDs)
}

// stringSet returns the distinct values of a list of strings, reporting false
// while the list or any of its elements is unknown. A null list is empty.
func stringSet(list types.List) (map[string]bool, bool) {
	if list.IsUnknown() {
		return nil, false
	}
	set := map[string]bool{}
	for _, element := range list.Elements() {
		s, ok := element.(types.String)
		if !ok || s.IsUnknown() {
			return nil, false
		}
		if !s.IsNull() {
			set[s.ValueString()] = true
		}
	}
	return set, true
}

// writeOnlyPassword returns password_wo. Being write-only, it is null in the
// plan and in state, so it can only be read from the configuration.
func writeOnlyPassword(ctx context.Context, config tfsdk.Config, diags *diag.Diagnostics) (string, bool) {
	var value types.String
	diags.Append(config.GetAttribute(ctx, path.Root("password_wo"), &value)...)
	if value.IsNull() || value.IsUnknown() {
		return "", false
	}
	return value.ValueString(), true
}

// configuredPassword returns the password a new user is created with: password
// when it is set, or else password_wo.
func configuredPassword(ctx context.Context, config tfsdk.Config, plan userModel, diags *diag.Diagnostics) (string, bool) {
	if !plan.Password.IsNull() && !plan.Password.IsUnknown() {
		return plan.Password.ValueString(), true
	}
	if password, ok := writeOnlyPassword(ctx, config, diags); ok {
		return password, true
	}
	if !diags.HasError() {
		// The configuration validator rules this out; it only skips its check
		// while a value is still unknown.
		diags.AddAttributeError(
			path.Root("password"),
			"Missing password",
			"Exactly one of password and password_wo must be set to create a Homarr user.",
		)
	}
	return "", false
}

// passwordChange returns the password an update has to set, if any.
//
// password is compared with state like any other attribute. password_wo never
// reaches state, so it is sent only when password_wo_version changes — which
// also covers switching from password to password_wo along with a version.
func passwordChange(ctx context.Context, config tfsdk.Config, plan, state userModel, diags *diag.Diagnostics) (string, bool) {
	if !plan.Password.IsNull() {
		if plan.Password.Equal(state.Password) {
			return "", false
		}
		return plan.Password.ValueString(), true
	}
	if plan.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		return "", false
	}
	return writeOnlyPassword(ctx, config, diags)
}

// isInvalidPreviousPassword reports whether err is Homarr refusing the previous
// password of a password change, which it checks only when the API key's own
// user changes its password.
func isInvalidPreviousPassword(err error) bool {
	var apiErr *client.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden && apiErr.Message == "Invalid password"
}

// passwordChangeErrorDetail explains a refused previous password, which is
// otherwise a puzzling 403 for an admin key.
func passwordChangeErrorDetail(err error) string {
	if !isInvalidPreviousPassword(err) {
		return err.Error()
	}
	return "This user owns the provider's API key, so Homarr requires its current password before changing it, " +
		"and neither the password in Terraform state nor the configured one is current. Set the password to " +
		"the configured value in the Homarr UI and apply again, or manage this account with another admin's " +
		"API key. " + err.Error()
}

// readHeader fills in the header layout of a homarr_user, which only
// GET /api/users/preferences reports in a usable form.
//
// That endpoint arrived in Homarr 2.3.0, and the user has just been read, so a
// 404 means an older release, which has no layout to report. An unknown header
// becomes null with a warning, letting the apply complete. A header already in
// state is kept as last applied, also with a warning, in case Homarr was
// downgraded; a null one simply stays null.
func readHeader(ctx context.Context, c *client.Client, userID string, v *userPreferenceValues, diags *diag.Diagnostics) {
	prefs, err := c.GetUserPreferences(ctx, userID)
	if err == nil {
		v.headerFromAPI(prefs.HeaderPreferences)
		return
	}
	if !client.IsNotFound(err) {
		if v.Header.IsUnknown() {
			// State cannot hold an unknown value, not even alongside an error.
			v.Header = types.ObjectNull(headerAttrTypes)
		}
		diags.AddError("Unable to read the Homarr user's header layout", err.Error())
		return
	}

	switch {
	case v.Header.IsUnknown():
		v.Header = types.ObjectNull(headerAttrTypes)
		diags.AddWarning(
			"Homarr user header layout is unavailable",
			"GET /api/users/preferences answered 404 although the user exists, which means this Homarr "+
				"predates 2.3.0. header requires Homarr 2.3.0 or newer and has been left null.",
		)
	case !v.Header.IsNull():
		diags.AddWarning(
			"Homarr user header layout could not be refreshed",
			"GET /api/users/preferences answered 404 although the user exists, which means this Homarr "+
				"predates 2.3.0. header was left as last applied, so drift is not detected.",
		)
	}
}

// isCredentialsUser reports whether the account signs in with a Homarr
// password, the only kind homarr_user can manage. An empty provider, which no
// Homarr release is known to report, is given the benefit of the doubt.
func isCredentialsUser(user *client.UserDetail) bool {
	return isCredentialsProvider(user.Provider)
}

func isCredentialsProvider(provider string) bool {
	return provider == "" || provider == "credentials"
}

// addExternalUserImportError refuses to import an LDAP or OIDC account. Homarr
// answers 403 to any attempt to rename one or change its email or password, so
// homarr_user would fail on its first change, and only its preferences can be
// managed at all.
func addExternalUserImportError(diags *diag.Diagnostics, id, provider string) {
	diags.AddError(
		"Homarr user is not a credentials account",
		fmt.Sprintf("User %s signs in through %s. homarr_user only manages accounts that sign in with a "+
			"Homarr password: Homarr refuses to rename any other account or change its email or password. "+
			"Manage this user's preferences with homarr_user_preferences instead.", id, provider),
	)
}

// addExternalUserWarning flags an LDAP or OIDC account that is already in
// state, imported before the provider refused them. Reading it stays possible,
// so that a `removed` block or a destroy, both of which refresh the user
// first, can still take it out of Terraform's hands.
func addExternalUserWarning(diags *diag.Diagnostics, user *client.UserDetail) {
	diags.AddWarning(
		"Homarr user is not a credentials account",
		fmt.Sprintf("User %s signs in through %s. homarr_user only manages accounts that sign in with a "+
			"Homarr password: Homarr refuses to rename any other account or change its email or password, so "+
			"a plan that would do so is refused. Stop managing this user here with a `removed` block "+
			"(`lifecycle { destroy = false }`) or `terraform state rm`, and manage its preferences with "+
			"homarr_user_preferences instead.", user.ID, user.Provider),
	)
}

// refuseExternalAccountChanges stops a plan that renames an LDAP or OIDC
// account, or changes its email or password, before Homarr answers the apply
// with a 403. Preferences and a replacement remain possible: Homarr applies
// the former to any account, and the latter deletes the account and creates a
// credentials one.
func refuseExternalAccountChanges(plan, state userModel, diags *diag.Diagnostics) {
	if state.Provider.IsNull() || state.Provider.IsUnknown() || isCredentialsProvider(state.Provider.ValueString()) {
		return
	}
	var changes []string
	if !plan.Username.Equal(state.Username) {
		changes = append(changes, "username")
	}
	if !plan.Email.IsNull() && !plan.Email.Equal(state.Email) {
		changes = append(changes, "email")
	}
	if !plan.Password.IsNull() && !plan.Password.Equal(state.Password) {
		changes = append(changes, "password")
	}
	if !plan.PasswordWOVersion.Equal(state.PasswordWOVersion) {
		changes = append(changes, "password_wo_version")
	}
	if len(changes) == 0 {
		return
	}
	diags.AddError(
		"Homarr user is not a credentials account",
		fmt.Sprintf("User %s signs in through %s, and Homarr refuses to change the username, email or password "+
			"of such an account. This plan changes %s. Stop managing this user here with a `removed` block "+
			"(`lifecycle { destroy = false }`) or `terraform state rm`, and manage its preferences with "+
			"homarr_user_preferences instead.",
			state.ID.ValueString(), state.Provider.ValueString(), strings.Join(changes, ", ")),
	)
}

// refuseAPIKeyOwner reports an error when id is the user the provider's API key
// belongs to. Homarr lets a user delete itself and deletes its API keys with
// it, so Terraform would lose access to Homarr partway through the apply.
//
// The owner is found through GET /api/users/preferences, which needs Homarr
// 2.3.0. On an older release the check is skipped; any other failure blocks
// the delete, which is safer than guessing.
func (r *userResource) refuseAPIKeyOwner(ctx context.Context, id string, diags *diag.Diagnostics) {
	owner, err := r.client.CurrentUserID(ctx)
	switch {
	case err == nil:
		if owner == id {
			addAPIKeyOwnerError(diags, id)
		}
	case client.IsNotFound(err):
		tflog.Debug(ctx, "Homarr predates 2.3.0 and cannot tell which user owns the API key; deleting the user unchecked",
			map[string]any{"user_id": id})
	default:
		diags.AddError(
			"Unable to check whether the Homarr user owns the API key",
			"Homarr deletes a user's API keys along with the account, so the provider makes sure its own key "+
				"does not belong to a user before deleting it. That check failed, and the user was left in "+
				"place. "+err.Error(),
		)
	}
}

func addAPIKeyOwnerError(diags *diag.Diagnostics, id string) {
	diags.AddError(
		"Refusing to delete the API key's own Homarr user",
		"User "+id+" owns the API key this provider is using. Homarr deletes a user's API keys along with the "+
			"account, which would cut Terraform off from Homarr in the middle of the apply. To stop managing "+
			"the account without deleting it, use a `removed` block with `destroy = false`, or `terraform "+
			"state rm`. To delete it, configure the provider with another admin's API key first.",
	)
}

// addAPIKeyOwnerReplacementError refuses a plan that replaces the API key's own
// user, which deletes it just the same.
func addAPIKeyOwnerReplacementError(diags *diag.Diagnostics, id string) {
	diags.AddError(
		"Refusing to replace the API key's own Homarr user",
		"User "+id+" owns the API key this provider is using, and this plan replaces it — because its email "+
			"is removed, which Homarr cannot do in place, or because its groups change, which are create-only. "+
			"Replacing a user deletes it, and Homarr deletes a user's API keys along with the account, which "+
			"would cut Terraform off from Homarr in the middle of the apply. Keep the email and groups as they "+
			"are, or configure the provider with another admin's API key first.",
	)
}

// credentialsUserIDByName resolves a "username=<name>" import to the id of the
// credentials account of that name. Names are unique only per auth provider,
// and GET /api/users does not report the provider, so every user of that name
// is read to find the credentials one.
func (r *userResource) credentialsUserIDByName(ctx context.Context, name string, diags *diag.Diagnostics) string {
	if name == "" {
		diags.AddError(
			"Unexpected import identifier",
			"Import a user by id, or by name as \"username=<name>\"; the name must not be empty.",
		)
		return ""
	}

	users, err := r.client.ListUsers(ctx)
	if err != nil {
		diags.AddError("Unable to look up the Homarr user to import", err.Error())
		return ""
	}

	var credentials, external []string
	for _, summary := range users {
		if summary.Name == nil || *summary.Name != name {
			continue
		}
		user, err := r.client.GetUser(ctx, summary.ID)
		if err != nil {
			diags.AddError("Unable to look up the Homarr user to import", err.Error())
			return ""
		}
		if isCredentialsUser(user) {
			credentials = append(credentials, user.ID)
		} else {
			external = append(external, user.Provider)
		}
	}

	switch {
	case len(credentials) == 1:
		return credentials[0]
	case len(credentials) > 1:
		diags.AddError(
			"Ambiguous import identifier",
			fmt.Sprintf("Several credentials users are named %q (ids %s). Import the right one by id.",
				name, strings.Join(credentials, ", ")),
		)
	case len(external) > 0:
		diags.AddError(
			"Homarr user is not a credentials account",
			fmt.Sprintf("The only users named %q sign in through %s, and homarr_user only manages accounts "+
				"that sign in with a Homarr password. Manage their preferences with homarr_user_preferences "+
				"instead.", name, strings.Join(external, " and ")),
		)
	default:
		diags.AddError(
			"Homarr user not found",
			fmt.Sprintf("No Homarr user is named %q. Homarr stores usernames in lowercase.", name),
		)
	}
	return ""
}
