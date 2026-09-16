package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// Revue sécurité 2026-09-16 — garde-fous légers : le provider s'instancie, son
// schéma se construit sans erreur, et chaque resource/data source enregistré
// expose bien un schéma valide. Ça attrape un schéma cassé au build, pas au
// premier `terraform plan` d'un utilisateur.

func newProvider(t *testing.T) provider.Provider {
	t.Helper()
	return New("test")()
}

func TestProviderMetadata(t *testing.T) {
	var resp provider.MetadataResponse
	newProvider(t).Metadata(context.Background(), provider.MetadataRequest{}, &resp)
	if resp.TypeName != "okstatus" {
		t.Fatalf("TypeName = %q, attendu okstatus", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("Version = %q, attendu test", resp.Version)
	}
}

func TestProviderSchemaNoError(t *testing.T) {
	var resp provider.SchemaResponse
	newProvider(t).Schema(context.Background(), provider.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schéma du provider en erreur: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["api_key"]; !ok {
		t.Fatal("attribut api_key manquant du schéma")
	}
	if _, ok := resp.Schema.Attributes["endpoint"]; !ok {
		t.Fatal("attribut endpoint manquant du schéma")
	}
}

func TestResourcesAndDataSourcesSchemas(t *testing.T) {
	p := newProvider(t)
	ctx := context.Background()

	resources := p.Resources(ctx)
	if len(resources) == 0 {
		t.Fatal("aucune resource enregistrée")
	}
	for _, factory := range resources {
		r := factory()
		var mResp resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "okstatus"}, &mResp)

		var sResp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sResp)
		if sResp.Diagnostics.HasError() {
			t.Fatalf("schéma de %s en erreur: %v", mResp.TypeName, sResp.Diagnostics)
		}
	}

	dataSources := p.DataSources(ctx)
	if len(dataSources) == 0 {
		t.Fatal("aucun data source enregistré")
	}
}
