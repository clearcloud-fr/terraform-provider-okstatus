// Terraform provider for OKStatus (okstatus.eu) — manage monitors,
// incidents, status pages, alert channels and maintenances as code against
// api-v1 (ok-status/apps/api/src/routes/api-v1.ts).
package main

import (
	"context"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/provider"
)

// version is overridden at build time via -ldflags "-X main.version=x.y.z"
// (GoReleaser does this automatically for tagged releases); "dev" for local
// builds.
var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/clearcloud-fr/okstatus",
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
