package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// These tests cover the plans homarr_user refuses before an apply: replacing
// the API key's own user, and changing what Homarr never changes for an LDAP or
// OIDC account.

func runUserModifyPlan(t *testing.T, r *userResource, prior, plan userModel) resource.ModifyPlanResponse {
	t.Helper()
	s := resourceSchema(t, r)
	planned := planOf(t, s, &plan)
	resp := resource.ModifyPlanResponse{Plan: planned}
	r.ModifyPlan(context.Background(), resource.ModifyPlanRequest{
		State:  stateOf(t, s, &prior),
		Plan:   planned,
		Config: configOf(t, s, &plan),
	}, &resp)
	return resp
}

// The attribute plan modifiers decide on the replacement, but a resource's
// ModifyPlan never sees their verdict, so the plan is checked on its own.
func TestUserPlanRefusesToReplaceTheAPIKeysOwnUser(t *testing.T) {
	withEmail := userTestModel()
	withEmail.Email = types.StringValue("alice@example.com")
	emailRemoved := withEmail
	emailRemoved.Email = types.StringNull()
	groupAdded := userTestModel()
	groupAdded.GroupIDs = groupList("g1")

	for _, tc := range []struct {
		name        string
		prior, plan userModel
		owner       string
		wantErr     bool
	}{
		{"email removed from the key's user", withEmail, emailRemoved, "u1", true},
		{"group added to the key's user", userTestModel(), groupAdded, "u1", true},
		{"email removed from another user", withEmail, emailRemoved, "admin", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply("GET /api/users/preferences", 200, `{"userId":"`+tc.owner+`"}`)
			r := &userResource{client: fake.client(t)}

			resp := runUserModifyPlan(t, r, tc.prior, tc.plan)
			if tc.wantErr {
				requireErrorContaining(t, resp.Diagnostics, "Refusing to replace the API key's own Homarr user")
				return
			}
			requireNoErrors(t, resp.Diagnostics)
		})
	}
}

// An in-place change cannot delete anyone, so it costs no request.
func TestUserPlanOnlyChecksTheOwnerForAReplacement(t *testing.T) {
	fake := newFakeHomarr()
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	plan := prior
	plan.Username = types.StringValue("bob")

	requireNoErrors(t, runUserModifyPlan(t, r, prior, plan).Diagnostics)
	if calls := fake.callLog(); len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
	}
}

func TestUserPlanRefusesProfileChangesToAnLDAPOrOIDCAccount(t *testing.T) {
	prior := userTestModel()
	prior.Provider = types.StringValue("oidc")
	prior.Password = types.StringNull() // as imported

	renamed := prior
	renamed.Username = types.StringValue("bob")
	withPassword := prior
	withPassword.Password = types.StringValue("Correct-Horse-1")
	withEmail := prior
	withEmail.Email = types.StringValue("alice@example.com")
	lightScheme := prior
	lightScheme.ColorScheme = types.StringValue("light")

	for _, tc := range []struct {
		name string
		plan userModel
		want string
	}{
		{"a rename", renamed, "username"},
		{"a password", withPassword, "password"},
		{"an email", withEmail, "email"},
		{"a preference", lightScheme, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := runUserModifyPlan(t, &userResource{}, prior, tc.plan)
			if tc.want == "" {
				requireNoErrors(t, resp.Diagnostics)
				return
			}
			requireErrorContaining(t, resp.Diagnostics, "signs in through oidc")
			requireErrorContaining(t, resp.Diagnostics, "This plan changes "+tc.want)
		})
	}
}
