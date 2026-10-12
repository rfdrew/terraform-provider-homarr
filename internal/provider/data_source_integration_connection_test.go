package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const integrationConnectionRoute = "GET /api/integrations/sonarr/test-connection"

func readIntegrationConnection(t *testing.T, fake *fakeHomarr) (integrationConnectionDataSourceModel, diag.Diagnostics) {
	t.Helper()
	var got integrationConnectionDataSourceModel
	diags := readDataSourceForTest(t, &integrationConnectionDataSource{client: fake.client(t)},
		&integrationConnectionDataSourceModel{
			IntegrationID: types.StringValue("sonarr"),
			Success:       types.BoolNull(),
			Error:         types.StringNull(),
		}, &got)
	return got, diags
}

func TestIntegrationConnectionDataSourceReportsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		body    string
		success bool
		err     types.String
	}{
		{`{"success":true}`, true, types.StringNull()},
		{`{"success":false,"error":"authorization"}`, false, types.StringValue("authorization")},
	} {
		fake := newFakeHomarr()
		fake.reply(integrationConnectionRoute, 200, tc.body)

		got, diags := readIntegrationConnection(t, fake)
		// A failed connection is a result to assert on, not a failed read.
		requireNoErrors(t, diags)
		if got.Success.ValueBool() != tc.success || !got.Error.Equal(tc.err) {
			t.Errorf("for %s: success = %v, error = %v; want %v, %v", tc.body, got.Success, got.Error, tc.success, tc.err)
		}
	}
}

func TestIntegrationConnectionDataSourceBlamesAnOldHomarrFor404(t *testing.T) {
	fake := newFakeHomarr() // the test-connection route is unrouted, so 404
	fake.reply("GET /api/info", 200, `{"version":"2.0.4"}`)

	_, diags := readIntegrationConnection(t, fake)
	requireErrorContaining(t, diags, "Requires Homarr 2.1.0 or newer")
	requireErrorContaining(t, diags, "reports version 2.0.4")
}

// On a release that has the route, a 404 is Homarr hiding an integration the
// key cannot fully access, or one that does not exist — not a version problem.
func TestIntegrationConnectionDataSourceReportsAMissingIntegration(t *testing.T) {
	for _, tc := range []struct {
		version     string
		mayBeTooOld bool
	}{
		{`{"version":"2.4.1"}`, false},
		{`{"version":"custom"}`, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			fake := newFakeHomarr()
			fake.reply(integrationConnectionRoute, 404, `{"message":"Integration not found","code":"NOT_FOUND"}`)
			fake.reply("GET /api/info", 200, tc.version)

			_, diags := readIntegrationConnection(t, fake)
			requireErrorContaining(t, diags, "Homarr integration not found")
			if diags.ErrorsCount() != 1 {
				t.Fatalf("want exactly one error, got %v", diags)
			}
			withPath, ok := diags.Errors()[0].(diag.DiagnosticWithPath)
			if !ok || !withPath.Path().Equal(path.Root("integration_id")) {
				t.Fatalf("error is not attached to integration_id: %v", diags)
			}
			if got := strings.Contains(withPath.Detail(), "predates 2.1.0"); got != tc.mayBeTooOld {
				t.Fatalf("mentions an old Homarr = %v, want %v: %v", got, tc.mayBeTooOld, diags)
			}
		})
	}
}

func TestIntegrationConnectionDataSourcePassesOtherErrorsThrough(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(integrationConnectionRoute, 401, `{"message":"Unauthorized","code":"UNAUTHORIZED"}`)

	_, diags := readIntegrationConnection(t, fake)
	requireErrorContaining(t, diags, "Unable to test Homarr integration connection")
	if fake.called("GET /api/info") {
		t.Fatal("asked for the version although the error was not a 404")
	}
}

// --- acceptance ---------------------------------------------------------------

func TestAccIntegrationConnectionDataSource_unknownIntegration(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "homarr_integration_connection" "missing" {
  integration_id = "tfacc-no-such-integration"
}
`,
				ExpectError: regexpMustCompile(`Homarr integration not found`),
			},
		},
	})
}

// TestAccIntegrationConnectionDataSource tests a real integration, which the
// throwaway test instance does not have: integrations cannot be created over
// REST. Set HOMARR_TEST_INTEGRATION_ID to the id of one to run it. Only the
// shape of the answer is checked, since the outcome depends on the service.
func TestAccIntegrationConnectionDataSource(t *testing.T) {
	integrationID := os.Getenv("HOMARR_TEST_INTEGRATION_ID")
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if integrationID == "" {
				t.Skip("HOMARR_TEST_INTEGRATION_ID is not set")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
data "homarr_integration_connection" "test" {
  integration_id = %q
}
`, integrationID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.homarr_integration_connection.test", "integration_id", integrationID),
					resource.TestCheckResourceAttrSet("data.homarr_integration_connection.test", "success"),
				),
			},
		},
	})
}
