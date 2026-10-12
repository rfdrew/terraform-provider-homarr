package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// These tests cover the `image` attribute of homarr_user and
// homarr_user_preferences: what Homarr accepts, when PUT
// /api/users/profileImage is sent, and how "" reads back.

const testImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAwS2OUAAAAABJRU5ErkJggg=="

func TestUserConfigValidatesTheImage(t *testing.T) {
	base := userConfigOf(readUserState())
	for _, tc := range []struct {
		name  string
		image string
		valid bool
	}{
		{"a PNG data URL", testImage, true},
		{"a WebP data URL", "data:image/webp;base64,UklGRg==", true},
		{"no picture", "", true},
		{"an https URL", "https://example.com/avatar.png", false},
		{"an SVG data URL", "data:image/svg+xml;base64,PHN2Zz4=", false},
		{"base64 with a line break", "data:image/png;base64,iVBO\nRw0K", false},
		{"too long", "data:image/png;base64," + strings.Repeat("A", 350000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := base
			config.Image = types.StringValue(tc.image)
			diags := validateUserConfig(t, config)
			if anyError(diags) == tc.valid {
				t.Fatalf("valid = %v, diagnostics: %v", tc.valid, diags)
			}
			// A rejected picture can be huge; the error quotes only its start.
			for _, d := range diags {
				if len(d.Detail) > 1000 {
					t.Errorf("error detail is %d characters long", len(d.Detail))
				}
			}
		})
	}
}

func TestUserCreateSetsAConfiguredImage(t *testing.T) {
	fake := createdUserFake(t)
	fake.reply("PUT /api/users/profileImage", 200, ``)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"email":null`, `"image":"`+testImage+`"`, 1))
	r := &userResource{client: fake.client(t)}

	plan := userCreatePlan()
	plan.Image = types.StringValue(testImage)
	resp := runUserCreate(t, r, plan, plan)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PUT /api/users/profileImage")
	if body["userId"] != "u1" || body["image"] != testImage {
		t.Fatalf("profileImage body = %v, want the new user's id and the picture", body)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if state.Image.ValueString() != testImage {
		t.Fatalf("image = %v, want the picture read back", state.Image)
	}
}

// A new user has no picture, so asking for none sends nothing.
func TestUserCreateWithNoImageSendsNone(t *testing.T) {
	for name, image := range map[string]types.String{"unconfigured": types.StringUnknown(), "empty": types.StringValue("")} {
		t.Run(name, func(t *testing.T) {
			fake := createdUserFake(t)
			r := &userResource{client: fake.client(t)}

			plan := userCreatePlan()
			plan.Image = image
			resp := runUserCreate(t, r, plan, plan)
			requireNoErrors(t, resp.Diagnostics)
			if fake.called("PUT /api/users/profileImage") {
				t.Fatal("set a picture nobody asked for")
			}

			var state userModel
			requireNoErrors(t, resp.State.Get(context.Background(), &state))
			if want := map[bool]types.String{true: types.StringNull(), false: types.StringValue("")}[image.IsUnknown()]; !state.Image.Equal(want) {
				t.Fatalf("image = %v, want %v", state.Image, want)
			}
		})
	}
}

func TestUserUpdateRemovesTheImageWhenSetToEmpty(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PUT /api/users/profileImage", 200, ``)
	fake.reply("GET /api/users/u1", 200, userDetailJSON)
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	prior.Image = types.StringValue(testImage)
	plan := prior
	plan.Image = types.StringValue("")

	resp := runUserUpdate(t, r, prior, plan, plan)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PUT /api/users/profileImage")
	if image, present := body["image"]; !present || image != nil {
		t.Fatalf("profileImage body = %v, want an explicit null", body)
	}
	var state userModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.Image.Equal(types.StringValue("")) {
		t.Fatalf("image = %v, want the configured \"\" kept for a picture Homarr reports as null", state.Image)
	}
}

func TestUserUpdateLeavesAnUnchangedImageAlone(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PUT /api/users/profile", 200, ``)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"email":null`, `"image":"`+testImage+`"`, 1))
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	r := &userResource{client: fake.client(t)}

	prior := userTestModel()
	prior.Image = types.StringValue(testImage)
	plan := prior
	plan.Username = types.StringValue("bob")

	requireNoErrors(t, runUserUpdate(t, r, prior, plan, plan).Diagnostics)
	if fake.called("PUT /api/users/profileImage") {
		t.Fatal("re-sent a picture that did not change")
	}
}

// An unconfigured picture is planned as it is, so that a rename does not show a
// 350,000-character "known after apply"; a null one, which state written before
// the attribute existed holds, is left for the apply to read.
func TestUserPlanKeepsAKnownImageAndLeavesANullOneUnknown(t *testing.T) {
	prior := readUserState()
	prior.Image = types.StringValue(testImage)
	config := userConfigOf(prior)
	config.Username = types.StringValue("bob")

	planned, _ := planUser(t, prior, config)
	if !planned.Image.Equal(prior.Image) {
		t.Fatalf("planned image = %v, want the prior picture kept", planned.Image)
	}

	prior.Image = types.StringNull()
	planned, _ = planUser(t, prior, config)
	if !planned.Image.IsUnknown() {
		t.Fatalf("planned image = %v, want unknown for a null prior picture", planned.Image)
	}
}

// --- homarr_user_preferences ------------------------------------------------------

func TestUserPreferencesRemovesTheImageOfAnyAccount(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 200, userPreferencesJSON)
	fake.reply("PUT /api/users/profileImage", 200, ``)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"provider":"credentials"`, `"provider":"oidc"`, 1))
	r := &userPreferencesResource{client: fake.client(t)}

	plan := userPreferencesCreatePlan(types.StringValue("u1"))
	plan.Image = types.StringValue("")
	resp := runUserPreferencesCreate(t, r, plan)
	requireNoErrors(t, resp.Diagnostics)

	body := fake.body(t, "PUT /api/users/profileImage")
	if image, present := body["image"]; !present || image != nil || body["userId"] != "u1" {
		t.Fatalf("profileImage body = %v, want u1 and an explicit null", body)
	}
	var state userPreferencesModel
	requireNoErrors(t, resp.State.Get(context.Background(), &state))
	if !state.Image.Equal(types.StringValue("")) {
		t.Fatalf("image = %v, want the configured \"\" kept", state.Image)
	}
}

func TestUserPreferencesReadsTheImageFromTheUser(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/users/preferences", 200, userPreferencesJSON)
	fake.reply("GET /api/users/u1", 200, strings.Replace(userDetailJSON, `"email":null`, `"image":"`+testImage+`"`, 1))
	r := &userPreferencesResource{client: fake.client(t)}

	state, _ := userPreferencesReadState(t)
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	var read userPreferencesModel
	requireNoErrors(t, resp.State.Get(context.Background(), &read))
	if read.Image.ValueString() != testImage {
		t.Fatalf("image = %v, want the picture from GET /api/users/u1", read.Image)
	}
}

// A picture the plan already knows, unchanged, needs neither a write nor a read.
func TestUserPreferencesLeavesAKnownUnchangedImageAlone(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("PATCH /api/users/preferences", 200, userPreferencesJSON)
	r := &userPreferencesResource{client: fake.client(t)}

	var diags diag.Diagnostics
	plan := userPreferencesCreatePlan(types.StringValue("u1"))
	plan.Image = types.StringValue(testImage)
	r.applyImage(context.Background(), &plan, types.StringValue(testImage), &diags)

	requireNoErrors(t, diags)
	if calls := fake.callLog(); len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
	}
}
