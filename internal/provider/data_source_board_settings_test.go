package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const boardSettingsRoute = "GET /api/boards/b1/settings"

func readBoardSettingsDataSource(t *testing.T, fake *fakeHomarr) (boardSettingsDataSourceModel, diag.Diagnostics) {
	t.Helper()
	var got boardSettingsDataSourceModel
	diags := readDataSourceForTest(t, &boardSettingsDataSource{client: fake.client(t)},
		&boardSettingsDataSourceModel{ID: types.StringValue("b1")}, &got)
	return got, diags
}

func TestBoardSettingsDataSourceReadsTheSettings(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(boardSettingsRoute, 200, `{"id":"b1","name":"infra","pageTitle":"Infra","opacity":80,`+
		`"disableStatus":true}`)

	got, diags := readBoardSettingsDataSource(t, fake)
	requireNoErrors(t, diags)
	if got.Name.ValueString() != "infra" || got.PageTitle.ValueString() != "Infra" || got.Opacity.ValueInt64() != 80 ||
		!got.DisableStatus.ValueBool() {
		t.Fatalf("settings = %+v, want the values Homarr returned", got)
	}
}

// Before 2.3.0 the route is missing, which Homarr answers with the same 404 as
// a missing board; the version tells the two apart.
func TestBoardSettingsDataSourceBlamesAnOldHomarrFor404(t *testing.T) {
	fake := newFakeHomarr() // the settings route is unrouted, so 404
	fake.reply("GET /api/info", 200, `{"version":"2.2.0"}`)

	_, diags := readBoardSettingsDataSource(t, fake)
	requireErrorContaining(t, diags, "Requires Homarr 2.3.0 or newer")
	requireErrorContaining(t, diags, "reports version 2.2.0")
}

func TestBoardSettingsDataSourceReportsAMissingBoard(t *testing.T) {
	for _, tc := range []struct {
		version     string
		mayBeTooOld bool
	}{
		{`{"version":"2.4.1"}`, false},
		{`{"version":"custom"}`, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply(boardSettingsRoute, 404, `{"message":"Board not found","code":"NOT_FOUND"}`)
			fake.reply("GET /api/info", 200, tc.version)

			_, diags := readBoardSettingsDataSource(t, fake)
			requireErrorContaining(t, diags, "Homarr board settings not found")
			if diags.ErrorsCount() != 1 {
				t.Fatalf("want exactly one error, got %v", diags)
			}
			if got := strings.Contains(diags.Errors()[0].Detail(), "predates 2.3.0"); got != tc.mayBeTooOld {
				t.Fatalf("mentions an old Homarr = %v, want %v: %v", got, tc.mayBeTooOld, diags)
			}
		})
	}
}

func TestBoardSettingsDataSourcePassesOtherErrorsThrough(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(boardSettingsRoute, 401, `{"message":"Unauthorized","code":"UNAUTHORIZED"}`)

	_, diags := readBoardSettingsDataSource(t, fake)
	requireErrorContaining(t, diags, "Unable to read Homarr board settings")
	if fake.called("GET /api/info") {
		t.Fatal("asked for the version although the error was not a 404")
	}
}
