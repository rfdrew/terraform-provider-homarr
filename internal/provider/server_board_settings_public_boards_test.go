package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Homarr requires both instance home boards to be public boards. 2.4.1 checks
// only the ids a PATCH sends; the release after it checks the stored result
// too, so a board the configuration never mentions — the private onboarding
// board, or one deleted since — can fail a change to something else entirely.

func refusedBoards(ids string) string {
	return `{"message":"Board settings home board IDs must reference public boards: ` + ids +
		`","code":"BAD_REQUEST"}`
}

// onboardingSettings is what onboarding leaves behind: its private board in
// both slots.
const onboardingSettings = `{"homeBoardId":"onboarding","mobileHomeBoardId":"onboarding",` +
	`"enableStatusByDefault":true,"forceDisableStatus":false}`

// createServerBoardSettings runs Create for a plan that sets only the given
// attributes and leaves the rest unconfigured, as Terraform plans them.
func createServerBoardSettings(t *testing.T, fake *fakeHomarr, configure func(m *serverBoardSettingsModel)) diag.Diagnostics {
	t.Helper()
	r := &serverBoardSettingsResource{client: fake.client(t)}
	s := resourceSchema(t, r)
	planned := serverBoardSettingsModel{
		ID:                    types.StringUnknown(),
		HomeBoardID:           types.StringUnknown(),
		MobileHomeBoardID:     types.StringUnknown(),
		EnableStatusByDefault: types.BoolUnknown(),
		ForceDisableStatus:    types.BoolUnknown(),
	}
	configure(&planned)

	resp := resource.CreateResponse{State: emptyServerBoardSettingsState(s)}
	r.Create(context.Background(), resource.CreateRequest{Plan: planOf(t, s, &planned)}, &resp)
	return resp.Diagnostics
}

// attributeErrors returns the errors on the two home board attributes, keyed
// by the attribute; errors without an attribute path are left out.
func attributeErrors(t *testing.T, diags diag.Diagnostics) map[string]diag.Diagnostic {
	t.Helper()
	byAttribute := map[string]diag.Diagnostic{}
	for _, d := range diags.Errors() {
		withPath, ok := d.(diag.DiagnosticWithPath)
		if !ok {
			continue
		}
		for _, name := range []string{"home_board_id", "mobile_home_board_id"} {
			if withPath.Path().Equal(path.Root(name)) {
				byAttribute[name] = d
			}
		}
	}
	return byAttribute
}

// Homarr 2.4.1: the configuration asked for a private board.
func TestServerBoardSettingsReportsARefusedBoardOnTheAttributeThatSentIt(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("priv"))

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("pub")
		m.MobileHomeBoardID = types.StringValue("priv")
	})

	errs := attributeErrors(t, diags)
	if len(errs) != 1 || diags.ErrorsCount() != 1 {
		t.Fatalf("want one error, on mobile_home_board_id; got %v", diags)
	}
	mobile, ok := errs["mobile_home_board_id"]
	if !ok || mobile.Summary() != "Home board is not a public board" || !strings.Contains(mobile.Detail(), "priv") {
		t.Fatalf("mobile_home_board_id error = %v, want the refused board priv named", mobile)
	}
	if fake.called("GET /api/settings/board") {
		t.Fatal("read the stored settings although the refused board was one this configuration sent")
	}
}

// One board in both slots is named twice by Homarr, and reported once per
// attribute that sent it.
func TestServerBoardSettingsReportsABoardSentForBothSlotsOnBoth(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("priv, priv"))

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("priv")
		m.MobileHomeBoardID = types.StringValue("priv")
	})

	errs := attributeErrors(t, diags)
	if len(errs) != 2 || diags.ErrorsCount() != 2 {
		t.Fatalf("want one error on each home board attribute, got %v", diags)
	}
}

// The release after 2.4.1: the configuration manages only a flag, and the
// stored onboarding board — private — is refused in both slots.
func TestServerBoardSettingsExplainsAStoredBoardItDidNotSend(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("onboarding, onboarding"))
	fake.reply("GET /api/settings/board", 200, onboardingSettings)

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.ForceDisableStatus = types.BoolValue(true)
	})

	errs := attributeErrors(t, diags)
	if len(errs) != 2 || diags.ErrorsCount() != 2 {
		t.Fatalf("want one error on each home board attribute, got %v", diags)
	}
	for name, d := range errs {
		if d.Summary() != "Stored home board is not a public board" {
			t.Errorf("%s summary = %q, want the stored-board explanation", name, d.Summary())
		}
		for _, want := range []string{"onboarding", "does not set " + name, `or to ""`, "public"} {
			if !strings.Contains(d.Detail(), want) {
				t.Errorf("%s detail does not mention %q: %s", name, want, d.Detail())
			}
		}
	}
}

// The release after 2.4.1, mixing both: the configuration fixes one slot, but
// the other still holds a deleted board.
func TestServerBoardSettingsSeparatesSentFromStoredBoards(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("gone"))
	fake.reply("GET /api/settings/board", 200, `{"homeBoardId":"old","mobileHomeBoardId":"gone",`+
		`"enableStatusByDefault":true,"forceDisableStatus":false}`)

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("pub")
	})

	errs := attributeErrors(t, diags)
	mobile, ok := errs["mobile_home_board_id"]
	if len(errs) != 1 || !ok || mobile.Summary() != "Stored home board is not a public board" {
		t.Fatalf("want only a stored-board error on mobile_home_board_id, got %v", diags)
	}
}

// The release after 2.4.1: the configuration sends the onboarding board for
// one slot, and the other, unmanaged, still holds it. Homarr names it twice,
// and both attributes are reported in one go rather than one per apply.
func TestServerBoardSettingsAlsoReportsTheSameBoardStoredInTheOtherSlot(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("onboarding, onboarding"))
	fake.reply("GET /api/settings/board", 200, onboardingSettings)

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("onboarding")
	})

	errs := attributeErrors(t, diags)
	if len(errs) != 2 || diags.ErrorsCount() != 2 {
		t.Fatalf("want one error on each home board attribute, got %v", diags)
	}
	if got := errs["home_board_id"].Summary(); got != "Home board is not a public board" {
		t.Errorf("home_board_id summary = %q, want the error for the board it sent", got)
	}
	if got := errs["mobile_home_board_id"].Summary(); got != "Stored home board is not a public board" {
		t.Errorf("mobile_home_board_id summary = %q, want the stored-board explanation", got)
	}
	if !strings.Contains(errs["mobile_home_board_id"].Detail(), "Homarr's UI") {
		t.Errorf("the stored-board detail does not say that the UI is blocked too: %s",
			errs["mobile_home_board_id"].Detail())
	}
}

// Homarr 2.4.1, same settings: it names only the board the patch sent, so the
// unmanaged slot is neither read nor blamed — 2.4.1 does not check it.
func TestServerBoardSettingsLeavesAStoredDuplicateAloneWhenHomarrDoesNotNameIt(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("onboarding"))
	fake.reply("GET /api/settings/board", 200, onboardingSettings)

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("onboarding")
	})

	errs := attributeErrors(t, diags)
	if _, ok := errs["home_board_id"]; len(errs) != 1 || !ok || diags.ErrorsCount() != 1 {
		t.Fatalf("want one error, on home_board_id; got %v", diags)
	}
	if fake.called("GET /api/settings/board") {
		t.Fatal("read the stored settings although Homarr named only the board this configuration sent")
	}
}

// Without the stored settings to tell which slot holds a refused board, it is
// still named, just not on an attribute — and once, though Homarr names it for
// both slots.
func TestServerBoardSettingsNamesARefusedBoardItCannotPlace(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("gone, gone"))
	// GET /api/settings/board is unrouted, so the stored settings are unknown.

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.EnableStatusByDefault = types.BoolValue(false)
	})

	if diags.ErrorsCount() != 1 {
		t.Fatalf("want exactly one error, got %v", diags)
	}
	if _, ok := diags.Errors()[0].(diag.DiagnosticWithPath); ok {
		t.Fatalf("an unplaceable board was pinned on an attribute: %v", diags)
	}
	requireErrorContaining(t, diags, "Homarr refused board gone")
}

func TestServerBoardSettingsKeepsOtherBadRequestsAsTheyAre(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, `{"message":"Input validation failed","code":"BAD_REQUEST"}`)

	diags := createServerBoardSettings(t, fake, func(m *serverBoardSettingsModel) {
		m.HomeBoardID = types.StringValue("pub")
	})

	requireErrorContaining(t, diags, "Unable to update Homarr server board settings")
	if len(attributeErrors(t, diags)) != 0 {
		t.Fatalf("an unrelated 400 was pinned on an attribute: %v", diags)
	}
}

func TestRefusedHomeBoardIDsParsesHomarrsMessage(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(serverBoardSettingsRoute, 400, refusedBoards("a, b, a"))
	_, err := fake.client(t).UpdateServerBoardSettings(context.Background(), nil)

	// Repeats are kept: Homarr names a board once per slot holding it.
	ids, ok := refusedHomeBoardIDs(err)
	if !ok || strings.Join(ids, " ") != "a b a" {
		t.Fatalf("refusedHomeBoardIDs() = %v, %v; want a, b and a again, in Homarr's order", ids, ok)
	}
	if _, ok := refusedHomeBoardIDs(context.Canceled); ok {
		t.Fatal("an error that is not from Homarr was taken for a refused board")
	}
}

// Destroying the resource leaves the home boards selected, which strands a
// board id when the same destroy deletes that board, so the warning names
// them. With none selected there is nothing to add.
func TestServerBoardSettingsDeleteWarnsAboutTheHomeBoardsItLeaves(t *testing.T) {
	r := &serverBoardSettingsResource{}
	s := resourceSchema(t, r)

	for _, tc := range []struct {
		name          string
		home, mobile  types.String
		wantBoards    []string
		wantHomeBoard bool
	}{
		{"both slots on one board", types.StringValue("b1"), types.StringValue("b1"), []string{"board b1"}, true},
		{"one slot cleared", types.StringValue("b1"), types.StringValue(""), []string{"board b1"}, true},
		{"two boards", types.StringValue("b1"), types.StringValue("b2"), []string{"board b1", "board b2"}, true},
		{"none selected", types.StringNull(), types.StringValue(""), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := stateOf(t, s, &serverBoardSettingsModel{
				ID:                    types.StringValue("board"),
				HomeBoardID:           tc.home,
				MobileHomeBoardID:     tc.mobile,
				EnableStatusByDefault: types.BoolValue(true),
				ForceDisableStatus:    types.BoolValue(false),
			})
			var resp resource.DeleteResponse
			r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)

			if resp.Diagnostics.HasError() || len(resp.Diagnostics.Warnings()) != 1 {
				t.Fatalf("want exactly one warning, got %v", resp.Diagnostics)
			}
			detail := resp.Diagnostics.Warnings()[0].Detail()
			for _, want := range tc.wantBoards {
				if !strings.Contains(detail, want) {
					t.Errorf("warning does not name %s: %s", want, detail)
				}
			}
			if got := strings.Contains(detail, "home boards stay selected"); got != tc.wantHomeBoard {
				t.Errorf("warning mentions the home boards = %v, want %v: %s", got, tc.wantHomeBoard, detail)
			}
			if strings.Count(detail, "board b1") > 1 {
				t.Errorf("warning names board b1 more than once: %s", detail)
			}
		})
	}
}
