package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// v1.6 — "private:<id d'agent>" est une région valide (data.okstatus_private_agents
// la fournit) ; l'ancien validateur OneOf la refusait dès le `plan`.
func TestRegionValidatorAcceptsPrivateAgent(t *testing.T) {
	cases := map[string]bool{
		"fr-dc3":  true,
		"private": true,
		"private:55555555-5555-4555-8555-555555555555": true,
		"private:not-an-id":                            false,
		"mars-1":                                       false,
	}
	for value, want := range cases {
		var resp validator.StringResponse
		regionValidator().ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("regions"),
			ConfigValue: types.StringValue(value),
		}, &resp)
		if got := !resp.Diagnostics.HasError(); got != want {
			t.Errorf("région %q: acceptée=%v, attendu %v", value, got, want)
		}
	}
}

// Les attributs ajoutés au moniteur ne doivent pas avoir de valeur par défaut :
// absents de la configuration, ils gardent ce que le moniteur a déjà (tags,
// pause, mot-clé posés depuis le tableau de bord), y compris après une montée
// de version du provider.
func TestMonitorSharedSettingsHaveNoDefault(t *testing.T) {
	var resp resource.SchemaResponse
	NewMonitorResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)

	for _, name := range []string{"tags", "paused", "muted", "keyword", "follow_redirects", "port", "dns_record_type", "dns_expected_value", "http_method", "http_body", "http_body_encoding", "ignore_tls",
		"retries", "severity_3xx", "keyword_invert", "json_path", "json_operator", "json_expected_value", "dns_server", "db_query", "child_monitor_ids"} {
		a, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("attribut %s manquant", name)
		}
		if !a.IsOptional() || !a.IsComputed() {
			t.Errorf("%s doit être Optional + Computed", name)
		}
		switch typed := a.(type) {
		case schema.BoolAttribute:
			if typed.Default != nil {
				t.Errorf("%s ne doit pas avoir de valeur par défaut", name)
			}
		case schema.StringAttribute:
			if typed.Default != nil {
				t.Errorf("%s ne doit pas avoir de valeur par défaut", name)
			}
		case schema.SetAttribute:
			if typed.Default != nil {
				t.Errorf("%s ne doit pas avoir de valeur par défaut", name)
			}
		case schema.Int64Attribute:
			if typed.Default != nil {
				t.Errorf("%s ne doit pas avoir de valeur par défaut", name)
			}
		}
	}
	// Non géré tant qu'il n'est pas écrit : ni Computed ni défaut.
	if a := resp.Schema.Attributes["alert_channel_ids"]; a == nil || a.IsComputed() || !a.IsOptional() {
		t.Error("alert_channel_ids doit être Optional seulement")
	}
	// Identifiants : jamais Computed (l'API les masque), secrets marqués sensibles.
	for _, name := range []string{"basic_auth_user", "basic_auth_password", "auth"} {
		if a := resp.Schema.Attributes[name]; a == nil || a.IsComputed() || !a.IsOptional() {
			t.Errorf("%s doit être Optional, non Computed", name)
		}
	}
	if !resp.Schema.Attributes["basic_auth_password"].IsSensitive() || !resp.Schema.Attributes["push_token"].IsSensitive() {
		t.Error("basic_auth_password et push_token doivent être sensibles")
	}
	auth := resp.Schema.Attributes["auth"].(schema.SingleNestedAttribute)
	for _, secret := range []string{"token", "value", "cookie", "client_secret", "key"} {
		if !auth.Attributes[secret].IsSensitive() {
			t.Errorf("auth.%s doit être sensible", secret)
		}
	}
	// Les en-têtes portent des jetons : jamais affichés dans un plan.
	if a := resp.Schema.Attributes["http_headers"]; a == nil || a.IsComputed() || !a.IsOptional() || !a.IsSensitive() {
		t.Error("http_headers doit être Optional, Sensitive, non Computed")
	}
}
