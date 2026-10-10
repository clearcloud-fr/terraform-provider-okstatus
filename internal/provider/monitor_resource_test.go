package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func tagSet(values ...string) types.Set {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.SetValueMust(types.StringType, elems)
}

// A monitor as the state holds it after a refresh.
func storedMonitor() monitorResourceModel {
	return monitorResourceModel{
		ID:                  types.StringValue("mon-1"),
		Name:                types.StringValue("API"),
		URL:                 types.StringValue("https://api.example.com"),
		Type:                types.StringValue("http"),
		Interval:            types.Int64Value(60),
		Regions:             types.ListNull(types.StringType),
		AcceptedStatusCodes: types.ListNull(types.StringType),
		Tags:                tagSet("prod"),
		Paused:              types.BoolValue(true),
		Muted:               types.BoolValue(true),
		Keyword:             types.StringValue("ok"),
		FollowRedirects:     types.BoolValue(false),
		Port:                types.Int64Null(),
		DNSRecordType:       types.StringValue(""),
		DNSExpectedValue:    types.StringValue(""),
		HTTPMethod:          types.StringValue("GET"),
		HTTPHeaders:         types.MapNull(types.StringType),
		HTTPBody:            types.StringValue(""),
		HTTPBodyEncoding:    types.StringValue(""),
		IgnoreTLS:           types.BoolValue(false),
		Retries:             types.Int64Value(1),
		Severity3xx:         types.StringValue("degraded"),
		KeywordInvert:       types.BoolValue(false),
		JSONPath:            types.StringValue(""),
		JSONOperator:        types.StringValue(""),
		JSONExpectedValue:   types.StringValue(""),
		DNSServer:           types.StringValue(""),
		DBQuery:             types.StringValue(""),
		GracePeriod:         types.Int64Null(),
		ChildMonitorIDs:     tagSet(),
		PushToken:           types.StringValue(""),
		BasicAuthUser:       types.StringNull(),
		BasicAuthPassword:   types.StringNull(),
		Auth:                types.ObjectNull(authAttrTypes),
		AlertChannelIDs:     types.SetNull(types.StringType),
		Status:              types.StringValue("paused"),
	}
}

func patchBody(t *testing.T, plan, prior monitorResourceModel) map[string]any {
	t.Helper()
	in, diags := monitorUpdateInput(context.Background(), plan, prior)
	if diags.HasError() {
		t.Fatalf("monitorUpdateInput: %v", diags)
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("encodage: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("décodage: %v", err)
	}
	return body
}

// Bug corrigé en v1.6 : sans `interval` dans la configuration, la valeur du
// plan est inconnue et partait comme 0 — l'API (minimum 30) refusait alors
// toute mise à jour du moniteur, même un simple renommage.
func TestMonitorUpdateDoesNotSendUnknownInterval(t *testing.T) {
	prior := storedMonitor()
	plan := storedMonitor()
	plan.Name = types.StringValue("API renamed")
	plan.Interval = types.Int64Unknown()

	body := patchBody(t, plan, prior)
	if _, present := body["interval"]; present {
		t.Fatalf("interval inconnu ne doit pas être envoyé: %v", body)
	}
}

// Un renommage ne touche à rien d'autre : ni l'état (un moniteur "down"
// repasserait "active"), ni les réglages posés depuis le tableau de bord.
func TestMonitorRenameSendsNothingElse(t *testing.T) {
	prior := storedMonitor()
	plan := storedMonitor()
	plan.Name = types.StringValue("API renamed")

	body := patchBody(t, plan, prior)
	want := map[string]bool{"name": true, "url": true, "interval": true}
	for key := range body {
		if !want[key] {
			t.Errorf("champ %q envoyé alors qu'il n'a pas changé: %v", key, body[key])
		}
	}
}

func TestMonitorUpdateSendsOnlyWhatChanged(t *testing.T) {
	prior := storedMonitor()
	plan := storedMonitor()
	plan.Paused = types.BoolValue(false)
	plan.Muted = types.BoolValue(false)
	plan.Tags = tagSet()
	plan.Keyword = types.StringValue("")
	plan.FollowRedirects = types.BoolValue(true)
	plan.Port = types.Int64Value(8443)
	plan.DNSRecordType = types.StringValue("TXT")
	plan.DNSExpectedValue = types.StringValue("v=spf1")

	body := patchBody(t, plan, prior)
	if body["status"] != "active" || body["isMuted"] != false {
		t.Errorf("reprise / réactivation des alertes: %v", body)
	}
	if tags, ok := body["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("`tags = []` doit partir comme liste vide: %v", body["tags"])
	}
	// "" dans la configuration = retirer le mot-clé : null explicite.
	if v, present := body["keyword"]; !present || v != nil {
		t.Errorf("keyword doit partir à null: %v", body)
	}
	if body["followRedirects"] != true || body["port"] != float64(8443) {
		t.Errorf("followRedirects/port: %v", body)
	}
	if body["dnsRecordType"] != "TXT" || body["dnsExpectedValue"] != "v=spf1" {
		t.Errorf("dns: %v", body)
	}
}

func headerMap(pairs map[string]string) types.Map {
	elems := map[string]attr.Value{}
	for k, v := range pairs {
		elems[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, elems)
}

// Options de requête HTTP (v1.6) : envoyées quand elles changent ; les
// en-têtes, jamais relus depuis l'API, ne repartent pas à chaque apply.
func TestMonitorUpdateSendsHTTPOptionsWhenTheyChange(t *testing.T) {
	prior := storedMonitor()
	plan := storedMonitor()
	plan.HTTPMethod = types.StringValue("POST")
	plan.HTTPBody = types.StringValue(`{"ping":true}`)
	plan.HTTPBodyEncoding = types.StringValue("json")
	plan.IgnoreTLS = types.BoolValue(true)
	plan.HTTPHeaders = headerMap(map[string]string{"Authorization": "Bearer t"})

	body := patchBody(t, plan, prior)
	if body["httpMethod"] != "POST" || body["httpBody"] != `{"ping":true}` || body["httpBodyEncoding"] != "json" || body["ignoreTls"] != true {
		t.Errorf("options HTTP: %v", body)
	}
	headers, ok := body["httpHeaders"].(map[string]any)
	if !ok || headers["Authorization"] != "Bearer t" {
		t.Errorf("en-têtes: %v", body["httpHeaders"])
	}

	// Même configuration à l'apply suivant : rien ne repart.
	again := patchBody(t, plan, plan)
	for _, k := range []string{"httpMethod", "httpBody", "httpBodyEncoding", "ignoreTls", "httpHeaders"} {
		if _, present := again[k]; present {
			t.Errorf("%s renvoyé sans changement", k)
		}
	}
}

func TestMonitorUpdateClearsBodyAndHeaders(t *testing.T) {
	prior := storedMonitor()
	prior.HTTPBody = types.StringValue("x")
	prior.HTTPHeaders = headerMap(map[string]string{"X-A": "1"})
	plan := prior
	plan.HTTPBody = types.StringValue("")
	plan.HTTPHeaders = headerMap(map[string]string{})

	body := patchBody(t, plan, prior)
	if v, present := body["httpBody"]; !present || v != nil {
		t.Errorf("httpBody doit partir à null: %v", body)
	}
	if headers, ok := body["httpHeaders"].(map[string]any); !ok || len(headers) != 0 {
		t.Errorf("`http_headers = {}` doit partir comme objet vide: %v", body["httpHeaders"])
	}
}

var authAttrTypes = map[string]attr.Type{
	"type": types.StringType, "token": types.StringType, "in": types.StringType, "name": types.StringType,
	"value": types.StringType, "cookie": types.StringType, "token_url": types.StringType,
	"client_id": types.StringType, "client_secret": types.StringType, "scope": types.StringType,
	"audience": types.StringType, "auth_style": types.StringType, "cert": types.StringType,
	"key": types.StringType, "ca": types.StringType,
}

func authBlock(values map[string]string) types.Object {
	elems := map[string]attr.Value{}
	for name := range authAttrTypes {
		if v, ok := values[name]; ok {
			elems[name] = types.StringValue(v)
		} else {
			elems[name] = types.StringNull()
		}
	}
	return types.ObjectValueMust(authAttrTypes, elems)
}

// Le bloc `auth` part avec les noms de champs de l'API (tokenUrl, clientId…)
// et seulement les attributs écrits.
func TestMonitorUpdateSendsAuthWithAPIFieldNames(t *testing.T) {
	prior := storedMonitor()
	prior.Auth = types.ObjectNull(authAttrTypes)
	plan := prior
	plan.Auth = authBlock(map[string]string{
		"type": "oauth2", "token_url": "https://login.example.com/token",
		"client_id": "probe", "client_secret": "s3cret", "auth_style": "post",
	})

	body := patchBody(t, plan, prior)
	auth, ok := body["authConfig"].(map[string]any)
	if !ok {
		t.Fatalf("authConfig absent: %v", body)
	}
	want := map[string]any{
		"type": "oauth2", "tokenUrl": "https://login.example.com/token",
		"clientId": "probe", "clientSecret": "s3cret", "authStyle": "post",
	}
	if len(auth) != len(want) {
		t.Errorf("champs inattendus: %v", auth)
	}
	for k, v := range want {
		if auth[k] != v {
			t.Errorf("authConfig.%s = %v, attendu %v", k, auth[k], v)
		}
	}

	// Inchangé à l'apply suivant : le secret ne repart pas.
	if _, present := patchBody(t, plan, plan)["authConfig"]; present {
		t.Error("authConfig renvoyé sans changement")
	}
}

// Retirer le bloc ou un identifiant de la configuration le retire du
// moniteur : un secret posé par Terraform ne doit pas rester derrière lui.
func TestMonitorUpdateRemovesCredentialsLeftOutOfTheConfiguration(t *testing.T) {
	prior := storedMonitor()
	prior.Auth = authBlock(map[string]string{"type": "bearer", "token": "t"})
	prior.BasicAuthUser = types.StringValue("u")
	prior.BasicAuthPassword = types.StringValue("p")
	prior.HTTPHeaders = headerMap(map[string]string{"X-A": "1"})
	plan := prior
	plan.Auth = types.ObjectNull(authAttrTypes)
	plan.BasicAuthUser = types.StringNull()
	plan.BasicAuthPassword = types.StringNull()
	plan.HTTPHeaders = types.MapNull(types.StringType)

	body := patchBody(t, plan, prior)
	for _, k := range []string{"authConfig", "basicAuthUser", "basicAuthPass"} {
		if v, present := body[k]; !present || v != nil {
			t.Errorf("%s doit partir à null: %v", k, body)
		}
	}
	if headers, ok := body["httpHeaders"].(map[string]any); !ok || len(headers) != 0 {
		t.Errorf("httpHeaders doit partir comme objet vide: %v", body["httpHeaders"])
	}
}

// Jamais géré par Terraform (moniteur importé, identifiants posés dans le
// tableau de bord) : rien n'est envoyé, donc rien n'est effacé.
func TestMonitorUpdateLeavesUnmanagedCredentialsAlone(t *testing.T) {
	prior := storedMonitor()
	prior.Auth = types.ObjectNull(authAttrTypes)
	plan := prior
	plan.Name = types.StringValue("renamed")

	body := patchBody(t, plan, prior)
	for _, k := range []string{"authConfig", "basicAuthUser", "basicAuthPass", "httpHeaders"} {
		if _, present := body[k]; present {
			t.Errorf("%s envoyé alors que Terraform ne le gère pas", k)
		}
	}
}

func TestMonitorUpdateRotatesOnlyThePassword(t *testing.T) {
	prior := storedMonitor()
	prior.BasicAuthUser = types.StringValue("probe")
	prior.BasicAuthPassword = types.StringValue("old")
	plan := prior
	plan.BasicAuthPassword = types.StringValue("new")

	body := patchBody(t, plan, prior)
	if body["basicAuthPass"] != "new" {
		t.Errorf("mot de passe: %v", body)
	}
	if _, present := body["basicAuthUser"]; present {
		t.Error("l'utilisateur n'a pas changé, il ne doit pas repartir")
	}
}

func TestMonitorUpdateSendsTypeSpecificSettings(t *testing.T) {
	prior := storedMonitor()
	prior.DBQuery = types.StringValue("SELECT 1")
	prior.JSONPath = types.StringValue("$.status")
	plan := prior
	plan.DBQuery = types.StringValue("")
	plan.JSONPath = types.StringValue("$.state")
	plan.JSONOperator = types.StringValue("==")
	plan.JSONExpectedValue = types.StringValue("ok")
	plan.Retries = types.Int64Value(3)
	plan.DNSServer = types.StringValue("1.1.1.1")
	plan.ChildMonitorIDs = tagSet("mon-a", "mon-b")

	body := patchBody(t, plan, prior)
	if v, present := body["dbQuery"]; !present || v != nil {
		t.Errorf("dbQuery doit partir à null: %v", body)
	}
	if body["jsonPath"] != "$.state" || body["jsonOperator"] != "==" || body["jsonExpectedValue"] != "ok" {
		t.Errorf("assertion JSON: %v", body)
	}
	if body["retries"] != float64(3) || body["dnsServer"] != "1.1.1.1" {
		t.Errorf("retries/dnsServer: %v", body)
	}
	if ids, ok := body["childMonitorIds"].([]any); !ok || len(ids) != 2 {
		t.Errorf("childMonitorIds: %v", body["childMonitorIds"])
	}
}

func TestURLWithCredentialsIsRefused(t *testing.T) {
	cases := map[string]bool{
		"postgres://db.example.com:5432/app":          true,
		"https://api.example.com/health?user=a@b.com": true,
		"example.com": true,
		"postgres://app:secret@db.example.com:5432/app": false,
		"https://user:pass@api.example.com":             false,
	}
	for value, want := range cases {
		var resp validator.StringResponse
		noURLCredentialsValidator{}.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("url"),
			ConfigValue: types.StringValue(value),
		}, &resp)
		if got := !resp.Diagnostics.HasError(); got != want {
			t.Errorf("url %q: acceptée=%v, attendu %v", value, got, want)
		}
	}
}
