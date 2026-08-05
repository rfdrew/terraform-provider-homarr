package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories wires the in-process provider into the
// acceptance test framework.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"homarr": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck fails fast when the acceptance environment is incomplete.
//
// Acceptance tests talk to a real Homarr instance and create and destroy real
// objects in it, so they only run when TF_ACC is set. Point HOMARR_URL and
// HOMARR_API_KEY at a throwaway instance, never a production one.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HOMARR_URL", "HOMARR_API_KEY"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s must be set for acceptance tests", name)
		}
	}
}

// providerConfig relies on HOMARR_URL and HOMARR_API_KEY being present in the
// environment, so no credentials appear in the test configurations.
const providerConfig = `provider "homarr" {}` + "\n"
