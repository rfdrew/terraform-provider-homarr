// terraform-provider-homarr manages a Homarr dashboard instance through its
// OpenAPI-compatible REST API.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/rfdrew/terraform-provider-homarr/internal/provider"
)

// version is overridden at release time via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/rfdrew/homarr",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
