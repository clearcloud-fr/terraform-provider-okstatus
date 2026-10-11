package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

// v1.6.1 — l'API renvoie les secrets d'un canal masqués. Réécrits dans
// l'état, ils différaient de la configuration à chaque plan ; `config`
// forçant le remplacement, chaque apply détruisait et recréait le canal (et
// la création elle-même finissait en « inconsistent result »).
func TestAlertChannelKeepsConfiguredSecretsOverMaskedOnes(t *testing.T) {
	configured := types.MapValueMust(types.StringType, map[string]attr.Value{
		"webhookUrl": types.StringValue("https://discord.com/api/webhooks/123/real-secret"),
	})
	data := alertChannelResourceModel{Config: configured}

	diags := writeAlertChannel(context.Background(), &data, &client.AlertChannel{
		ID: "ch-1", Type: "discord", Enabled: true,
		Config: map[string]any{"webhookUrl": "https://discord.com/api/webhooks/***"},
	})
	if diags.HasError() {
		t.Fatalf("writeAlertChannel: %v", diags)
	}
	if !data.Config.Equal(configured) {
		t.Fatalf("la config de l'état a été remplacée par la valeur masquée: %v", data.Config)
	}
	if data.ID.ValueString() != "ch-1" || !data.Enabled.ValueBool() {
		t.Fatalf("id/enabled non repris: %+v", data)
	}
}

// Canal importé : pas encore de configuration dans l'état, on prend ce que
// l'API donne (secrets masqués, booléens en chaînes).
func TestImportedAlertChannelTakesTheAPIConfig(t *testing.T) {
	data := alertChannelResourceModel{Config: types.MapNull(types.StringType)}

	diags := writeAlertChannel(context.Background(), &data, &client.AlertChannel{
		ID: "ch-1", Type: "jira", Enabled: true,
		Config: map[string]any{"projectKey": "OPS", "autoResolve": true},
	})
	if diags.HasError() {
		t.Fatalf("writeAlertChannel: %v", diags)
	}
	got := map[string]string{}
	data.Config.ElementsAs(context.Background(), &got, false)
	if got["projectKey"] != "OPS" || got["autoResolve"] != "true" {
		t.Fatalf("config importée: %v", got)
	}
}
