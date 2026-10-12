package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// homarr_server_board_settings spells "no home board" as "", because its
// attributes are Optional+Computed and a null there only means "not managed".
// Homarr wants an explicit null for it, and reports a cleared board as null.

const serverBoardSettingsRoute = "PATCH /api/settings/board"

func emptyServerBoardSettingsState(s schema.Schema) tfsdk.State {
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(context.Background()), nil)}
}

func TestServerBoardSettingsEmptyStringClearsAHomeBoard(t *testing.T) {
	for _, tc := range []struct {
		attribute, key string
		withEmpty      func(m *serverBoardSettingsModel)
		cleared        func(m serverBoardSettingsModel) types.String
		response       string
	}{
		{
			attribute: "home_board_id",
			key:       "homeBoardId",
			withEmpty: func(m *serverBoardSettingsModel) { m.HomeBoardID = types.StringValue("") },
			cleared:   func(m serverBoardSettingsModel) types.String { return m.HomeBoardID },
			response: `{"homeBoardId":null,"mobileHomeBoardId":"M","enableStatusByDefault":true,` +
				`"forceDisableStatus":false}`,
		},
		{
			attribute: "mobile_home_board_id",
			key:       "mobileHomeBoardId",
			withEmpty: func(m *serverBoardSettingsModel) { m.MobileHomeBoardID = types.StringValue("") },
			cleared:   func(m serverBoardSettingsModel) types.String { return m.MobileHomeBoardID },
			response: `{"homeBoardId":"H","mobileHomeBoardId":null,"enableStatusByDefault":true,` +
				`"forceDisableStatus":false}`,
		},
	} {
		t.Run(tc.attribute, func(t *testing.T) {
			ctx := context.Background()
			fake := newFakeHomarr()
			fake.reply(serverBoardSettingsRoute, 200, tc.response)
			r := &serverBoardSettingsResource{client: fake.client(t)}
			s := resourceSchema(t, r)

			// Homarr holds a board in both slots. The configuration sets this one
			// to "" and leaves everything else out, which Terraform plans as
			// unknown.
			state := stateOf(t, s, &serverBoardSettingsModel{
				ID:                    types.StringValue("board"),
				HomeBoardID:           types.StringValue("H"),
				MobileHomeBoardID:     types.StringValue("M"),
				EnableStatusByDefault: types.BoolValue(true),
				ForceDisableStatus:    types.BoolValue(false),
			})
			planned := serverBoardSettingsModel{
				ID:                    types.StringValue("board"),
				HomeBoardID:           types.StringUnknown(),
				MobileHomeBoardID:     types.StringUnknown(),
				EnableStatusByDefault: types.BoolUnknown(),
				ForceDisableStatus:    types.BoolUnknown(),
			}
			tc.withEmpty(&planned)

			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{Plan: planOf(t, s, &planned), State: state}, &resp)
			requireNoErrors(t, resp.Diagnostics)

			body := fake.body(t, serverBoardSettingsRoute)
			if want := map[string]any{tc.key: nil}; !reflect.DeepEqual(body, want) {
				t.Fatalf("PATCH body = %v, want only an explicit null %s and no key for the unmanaged fields", body, tc.key)
			}

			var got serverBoardSettingsModel
			requireNoErrors(t, resp.State.Get(ctx, &got))
			if v := tc.cleared(got); v.IsNull() || v.ValueString() != "" {
				t.Fatalf("%s = %v after the apply, want the configured \"\" kept", tc.attribute, v)
			}
		})
	}
}

func TestServerBoardSettingsReadKeepsEmptyStringForAClearedHomeBoard(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/settings/board", 200, `{"homeBoardId":null,"mobileHomeBoardId":null,`+
		`"enableStatusByDefault":true,"forceDisableStatus":false}`)
	r := &serverBoardSettingsResource{client: fake.client(t)}
	s := resourceSchema(t, r)

	// home_board_id was cleared with "", mobile_home_board_id was never managed.
	state := stateOf(t, s, &serverBoardSettingsModel{
		ID:                    types.StringValue("board"),
		HomeBoardID:           types.StringValue(""),
		MobileHomeBoardID:     types.StringNull(),
		EnableStatusByDefault: types.BoolValue(true),
		ForceDisableStatus:    types.BoolValue(false),
	})
	resp := resource.ReadResponse{State: state}
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	var got serverBoardSettingsModel
	requireNoErrors(t, resp.State.Get(ctx, &got))
	if got.HomeBoardID.IsNull() || got.HomeBoardID.ValueString() != "" {
		t.Fatalf("home_board_id = %v, want the \"\" in state kept for Homarr's null", got.HomeBoardID)
	}
	if !got.MobileHomeBoardID.IsNull() {
		t.Fatalf("mobile_home_board_id = %v, want null for an unmanaged, unset board", got.MobileHomeBoardID)
	}
}

func TestServerBoardSettingsLeavesUnconfiguredHomeBoardsOut(t *testing.T) {
	// An unconfigured attribute is planned as unknown; null is checked too,
	// because a null string reads as "" and must not turn into a clear.
	for name, unconfigured := range map[string]types.String{
		"unknown": types.StringUnknown(),
		"null":    types.StringNull(),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fake := newFakeHomarr()
			fake.reply(serverBoardSettingsRoute, 200, `{"homeBoardId":"H","mobileHomeBoardId":"M2",`+
				`"enableStatusByDefault":true,"forceDisableStatus":false}`)
			r := &serverBoardSettingsResource{client: fake.client(t)}
			s := resourceSchema(t, r)

			plan := planOf(t, s, &serverBoardSettingsModel{
				ID:                    types.StringUnknown(),
				HomeBoardID:           unconfigured,
				MobileHomeBoardID:     types.StringValue("M2"),
				EnableStatusByDefault: types.BoolValue(true),
				ForceDisableStatus:    types.BoolUnknown(),
			})
			resp := resource.CreateResponse{State: emptyServerBoardSettingsState(s)}
			r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
			requireNoErrors(t, resp.Diagnostics)

			body := fake.body(t, serverBoardSettingsRoute)
			want := map[string]any{"mobileHomeBoardId": "M2", "enableStatusByDefault": true}
			if !reflect.DeepEqual(body, want) {
				t.Fatalf("PATCH body = %v, want %v: an unconfigured attribute must not be sent at all", body, want)
			}

			var got serverBoardSettingsModel
			requireNoErrors(t, resp.State.Get(ctx, &got))
			if got.HomeBoardID.ValueString() != "H" {
				t.Fatalf("home_board_id = %v, want Homarr's H adopted", got.HomeBoardID)
			}
		})
	}
}

// Homarr accepts an empty patch, and the resource sends one when nothing is
// configured. It has to arrive as {}: a bare null fails Homarr's input schema.
func TestServerBoardSettingsWithNothingConfiguredSendsAnEmptyObject(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 200, `{"homeBoardId":null,"mobileHomeBoardId":null,`+
		`"enableStatusByDefault":true,"forceDisableStatus":false}`)
	c := fake.client(t)

	if _, err := c.UpdateServerBoardSettings(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if body := fake.body(t, serverBoardSettingsRoute); body == nil || len(body) != 0 {
		t.Fatalf("PATCH body for a nil patch = %#v, want an empty object", body)
	}

	r := &serverBoardSettingsResource{client: c}
	s := resourceSchema(t, r)
	plan := planOf(t, s, &serverBoardSettingsModel{
		ID:                    types.StringUnknown(),
		HomeBoardID:           types.StringUnknown(),
		MobileHomeBoardID:     types.StringUnknown(),
		EnableStatusByDefault: types.BoolUnknown(),
		ForceDisableStatus:    types.BoolUnknown(),
	})
	resp := resource.CreateResponse{State: emptyServerBoardSettingsState(s)}
	r.Create(ctx, resource.CreateRequest{Plan: plan}, &resp)
	requireNoErrors(t, resp.Diagnostics)

	if body := fake.body(t, serverBoardSettingsRoute); body == nil || len(body) != 0 {
		t.Fatalf("PATCH body with nothing configured = %#v, want an empty object", body)
	}
	var got serverBoardSettingsModel
	requireNoErrors(t, resp.State.Get(ctx, &got))
	if !got.HomeBoardID.IsNull() || !got.MobileHomeBoardID.IsNull() {
		t.Fatalf("home boards = %v / %v, want null for unmanaged, unset boards", got.HomeBoardID, got.MobileHomeBoardID)
	}
}
