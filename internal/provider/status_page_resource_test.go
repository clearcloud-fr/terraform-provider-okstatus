package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

// OKS-902 — sections d'une page de statut.

func section(t *testing.T, name string, ids ...string) client.StatusPageSection {
	t.Helper()
	if ids == nil {
		ids = []string{}
	}
	return client.StatusPageSection{Name: name, MonitorIDs: ids}
}

func sectionsList(t *testing.T, s ...client.StatusPageSection) types.List {
	t.Helper()
	var diags diag.Diagnostics
	v := sectionsValue(context.Background(), s, &diags)
	if diags.HasError() {
		t.Fatalf("sectionsValue: %v", diags)
	}
	return v
}

func TestSectionsRoundTripKeepsOrder(t *testing.T) {
	in := []client.StatusPageSection{section(t, "Web", "m2", "m1"), section(t, "Empty")}
	var diags diag.Diagnostics
	out := sectionsFromPlan(context.Background(), sectionsList(t, in...), &diags)
	if diags.HasError() {
		t.Fatalf("sectionsFromPlan: %v", diags)
	}
	got, _ := json.Marshal(out)
	if string(got) != `[{"name":"Web","monitorIds":["m2","m1"]},{"name":"Empty","monitorIds":[]}]` {
		t.Fatalf("aller-retour: %s", got)
	}
}

// validate runs ValidateConfig on a configuration holding these sections.
func validate(t *testing.T, sections types.List) diag.Diagnostics {
	t.Helper()
	ctx := context.Background()
	r := &StatusPageResource{}
	var sResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sResp)

	model := statusPageResourceModel{
		ID: types.StringNull(), Slug: types.StringValue("acme"), Title: types.StringValue("Acme"),
		Description: types.StringNull(), LogoURL: types.StringNull(), FaviconURL: types.StringNull(),
		AccentColor: types.StringNull(), Theme: types.StringNull(), CustomCSS: types.StringNull(),
		FooterText: types.StringNull(), Published: types.BoolNull(), AutoRefreshInterval: types.Int64Null(),
		CustomDomain: types.StringNull(), Sections: sections,
	}
	state := tfsdk.State{Schema: sResp.Schema}
	if d := state.Set(ctx, &model); d.HasError() {
		t.Fatalf("state.Set: %v", d)
	}
	var resp resource.ValidateConfigResponse
	r.ValidateConfig(ctx, resource.ValidateConfigRequest{Config: tfsdk.Config{Schema: sResp.Schema, Raw: state.Raw}}, &resp)
	return resp.Diagnostics
}

func TestValidateRefusesAMonitorInTwoSections(t *testing.T) {
	d := validate(t, sectionsList(t, section(t, "Web", "m1"), section(t, "API", "m2", "m1")))
	if !d.HasError() || !strings.Contains(d.Errors()[0].Detail(), "m1 is already in section 1") {
		t.Fatalf("doublon non signalé: %v", d)
	}
}

func TestValidateAcceptsDistinctMonitorsAndNoSections(t *testing.T) {
	if d := validate(t, sectionsList(t, section(t, "Web", "m1"), section(t, "API", "m2"))); d.HasError() {
		t.Fatalf("config valide refusée: %v", d)
	}
	if d := validate(t, types.ListNull(statusPageSectionType)); d.HasError() {
		t.Fatalf("sections absentes refusées: %v", d)
	}
}

func TestApplySections(t *testing.T) {
	var calls int
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		// Renvoie ce qui a été envoyé, comme l'API.
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	r := &StatusPageResource{client: client.New(srv.URL, "k")}
	ctx := context.Background()

	// Attribut absent : Terraform ne touche pas aux sections du tableau de bord.
	var diags diag.Diagnostics
	if v := r.applySections(ctx, "sp-1", types.ListNull(statusPageSectionType), &diags); !v.IsNull() || calls != 0 {
		t.Fatalf("absent: appel=%d valeur=%v", calls, v)
	}

	// [] vide la page.
	v := r.applySections(ctx, "sp-1", sectionsList(t), &diags)
	if diags.HasError() || calls != 1 || body != `{"sections":[]}` || v.IsNull() || len(v.Elements()) != 0 {
		t.Fatalf("liste vide: appel=%d corps=%s diags=%v", calls, body, diags)
	}

	planned := sectionsList(t, section(t, "Web", "m2", "m1"))
	v = r.applySections(ctx, "sp-1", planned, &diags)
	if diags.HasError() || !v.Equal(planned) {
		t.Fatalf("sections: %v diags=%v", v, diags)
	}
}

func TestRefreshSections(t *testing.T) {
	ctx := context.Background()
	api := []client.StatusPageSection{section(t, "Web", "m1")}
	var diags diag.Diagnostics

	// Non gérées (import, attribut absent) : restent null, pas de diff.
	data := statusPageResourceModel{Sections: types.ListNull(statusPageSectionType)}
	refreshSections(ctx, &data, &client.StatusPage{Sections: &api}, &diags)
	if !data.Sections.IsNull() {
		t.Fatal("sections non gérées écrites dans l'état")
	}

	// Gérées : l'état suit l'API (un changement fait dans le tableau de bord apparaît au plan).
	data.Sections = sectionsList(t)
	refreshSections(ctx, &data, &client.StatusPage{Sections: &api}, &diags)
	if !data.Sections.Equal(sectionsList(t, api...)) {
		t.Fatalf("état non rafraîchi: %v", data.Sections)
	}

	// API sans sections (antérieure à OKS-902) : l'état est gardé.
	refreshSections(ctx, &data, &client.StatusPage{}, &diags)
	if !data.Sections.Equal(sectionsList(t, api...)) || diags.HasError() {
		t.Fatalf("état perdu: %v %v", data.Sections, diags)
	}
}

// OKS-906 — un changement de titre seul envoyait published=false et
// autoRefreshInterval=0 : page dépubliée, ou PATCH refusé (minimum 30).
func TestStatusPageUpdateSendsOnlyKnownPublishedAndInterval(t *testing.T) {
	data := statusPageResourceModel{
		ID: types.StringValue("sp-1"), Title: types.StringValue("Acme 2"),
		Published: types.BoolUnknown(), AutoRefreshInterval: types.Int64Unknown(),
	}
	b, _ := json.Marshal(statusPageUpdateInput(data))
	if strings.Contains(string(b), "published") || strings.Contains(string(b), "autoRefreshInterval") {
		t.Fatalf("valeurs inconnues envoyées: %s", b)
	}

	data.Published = types.BoolValue(false)
	data.AutoRefreshInterval = types.Int64Value(120)
	b, _ = json.Marshal(statusPageUpdateInput(data))
	if !strings.Contains(string(b), `"published":false`) || !strings.Contains(string(b), `"autoRefreshInterval":120`) {
		t.Fatalf("valeurs explicites perdues: %s", b)
	}
}

func TestStatusPageKeepsPublishedAndIntervalFromState(t *testing.T) {
	var resp resource.SchemaResponse
	(&StatusPageResource{}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	for _, name := range []string{"published", "auto_refresh_interval"} {
		a := resp.Schema.Attributes[name]
		var n int
		switch v := a.(type) {
		case interface{ BoolPlanModifiers() []planmodifier.Bool }:
			n = len(v.BoolPlanModifiers())
		case interface{ Int64PlanModifiers() []planmodifier.Int64 }:
			n = len(v.Int64PlanModifiers())
		}
		if n == 0 {
			t.Fatalf("%s sans UseStateForUnknown : inconnu à chaque modification", name)
		}
	}
}
