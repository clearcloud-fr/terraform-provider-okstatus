package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Revue sécurité 2026-09-16 — le provider n'avait aucun test. Ces tests
// couvrent la couche client (auth, construction des requêtes, décodage,
// erreurs, pagination) sans API réelle, via httptest. Les tests d'acceptation
// (resource.Test / TF_ACC) restent à part : ils exigent une org OKStatus et
// une clé API vivantes.

// recordingServer capte la dernière requête reçue et répond avec un scénario fixe.
type recordingServer struct {
	srv        *httptest.Server
	gotMethod  string
	gotPath    string
	gotRawPath string
	gotAuth    string
	gotCT      string
	gotBody    []byte
	status     int
	respBody   string
}

func newServer(t *testing.T, status int, respBody string) *recordingServer {
	t.Helper()
	rs := &recordingServer{status: status, respBody: respBody}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.gotMethod = r.Method
		rs.gotPath = r.URL.Path
		rs.gotRawPath = r.URL.RequestURI()
		rs.gotAuth = r.Header.Get("Authorization")
		rs.gotCT = r.Header.Get("Content-Type")
		rs.gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(rs.status)
		_, _ = io.WriteString(w, rs.respBody)
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	c := New("https://api.okstatus.eu/api/v1/", "k")
	if c.endpoint != "https://api.okstatus.eu/api/v1" {
		t.Fatalf("endpoint non normalisé: %q", c.endpoint)
	}
	if New("", "k").endpoint != DefaultEndpoint {
		t.Fatalf("endpoint vide devrait retomber sur DefaultEndpoint")
	}
}

func TestCreateMonitorSendsBearerAndBody(t *testing.T) {
	rs := newServer(t, http.StatusCreated, `{"data":{"id":"mon-1","name":"API","url":"https://x"}}`)
	c := New(rs.srv.URL, "oks_live_secret")

	m, err := c.CreateMonitor(context.Background(), MonitorCreateInput{Name: "API", URL: "https://x"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if m.ID != "mon-1" {
		t.Fatalf("envelope data mal décodée: %+v", m)
	}
	if rs.gotMethod != http.MethodPost || rs.gotPath != "/monitors" {
		t.Fatalf("méthode/chemin: %s %s", rs.gotMethod, rs.gotPath)
	}
	if rs.gotAuth != "Bearer oks_live_secret" {
		t.Fatalf("en-tête Authorization: %q", rs.gotAuth)
	}
	if rs.gotCT != "application/json" {
		t.Fatalf("Content-Type manquant sur une requête avec corps: %q", rs.gotCT)
	}
	var sent MonitorCreateInput
	if err := json.Unmarshal(rs.gotBody, &sent); err != nil || sent.Name != "API" {
		t.Fatalf("corps envoyé inattendu: %s (%v)", rs.gotBody, err)
	}
}

func TestGetMonitorPathAndDecode(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"mon-42","name":"DB"}}`)
	c := New(rs.srv.URL, "k")
	m, err := c.GetMonitor(context.Background(), "mon-42")
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if rs.gotMethod != http.MethodGet || rs.gotPath != "/monitors/mon-42" {
		t.Fatalf("méthode/chemin: %s %s", rs.gotMethod, rs.gotPath)
	}
	if m.Name != "DB" {
		t.Fatalf("décodage: %+v", m)
	}
}

func TestUpdateMonitorUsesPatch(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"mon-1"}}`)
	c := New(rs.srv.URL, "k")
	name := "renommé"
	if _, err := c.UpdateMonitor(context.Background(), "mon-1", MonitorUpdateInput{Name: &name}); err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if rs.gotMethod != http.MethodPatch {
		t.Fatalf("update devrait être un PATCH, reçu %s", rs.gotMethod)
	}
}

func TestDeleteMonitorNoBody(t *testing.T) {
	rs := newServer(t, http.StatusNoContent, "")
	c := New(rs.srv.URL, "k")
	if err := c.DeleteMonitor(context.Background(), "mon-1"); err != nil {
		t.Fatalf("DeleteMonitor: %v", err)
	}
	if rs.gotMethod != http.MethodDelete || rs.gotPath != "/monitors/mon-1" {
		t.Fatalf("méthode/chemin: %s %s", rs.gotMethod, rs.gotPath)
	}
	if rs.gotCT != "" {
		t.Fatalf("pas de Content-Type attendu sans corps, reçu %q", rs.gotCT)
	}
}

func TestErrorEnvelopeBecomesAPIError(t *testing.T) {
	rs := newServer(t, http.StatusUnprocessableEntity, `{"error":{"code":"VALIDATION","message":"url required"}}`)
	c := New(rs.srv.URL, "k")
	_, err := c.CreateMonitor(context.Background(), MonitorCreateInput{Name: "x"})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("attendu *APIError, reçu %T (%v)", err, err)
	}
	if apiErr.StatusCode != 422 || apiErr.Code != "VALIDATION" || apiErr.Message != "url required" {
		t.Fatalf("APIError mal renseignée: %+v", apiErr)
	}
}

func TestErrorWithoutEnvelopeKeepsStatus(t *testing.T) {
	rs := newServer(t, http.StatusBadGateway, `<html>502</html>`)
	c := New(rs.srv.URL, "k")
	_, err := c.GetMonitor(context.Background(), "mon-1")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("attendu *APIError, reçu %T", err)
	}
	if apiErr.StatusCode != 502 || apiErr.Code != "" {
		t.Fatalf("un corps non-JSON ne doit pas empêcher l'APIError: %+v", apiErr)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{StatusCode: 404}) {
		t.Fatal("404 devrait être un NotFound")
	}
	if IsNotFound(&APIError{StatusCode: 500}) {
		t.Fatal("500 n'est pas un NotFound")
	}
	if IsNotFound(nil) {
		t.Fatal("nil n'est pas un NotFound")
	}
}

// ListMonitors doit parcourir toutes les pages, pas seulement la première.
func TestListMonitorsPaginates(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			_, _ = io.WriteString(w, `{"monitors":[{"id":"a"},{"id":"b"}],"pages":2}`)
		case "2":
			_, _ = io.WriteString(w, `{"monitors":[{"id":"c"}],"pages":2}`)
		default:
			t.Errorf("page inattendue: %q", page)
			_, _ = io.WriteString(w, `{"monitors":[],"pages":2}`)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "k")
	all, err := c.ListMonitors(context.Background())
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	if len(all) != 3 || all[0].ID != "a" || all[2].ID != "c" {
		t.Fatalf("pagination incomplète: %+v", all)
	}
	if calls != 2 {
		t.Fatalf("attendu 2 pages, %d appels", calls)
	}
}

// ─── v1.6 — réglages partagés avec le tableau de bord et le connecteur MCP ───

func TestUpdateMonitorSendsExplicitNullsAndEmptyTags(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"mon-1","status":"paused","tags":[]}}`)
	c := New(rs.srv.URL, "k")

	paused := "paused"
	muted := true
	noTags := []string{}
	_, err := c.UpdateMonitor(context.Background(), "mon-1", MonitorUpdateInput{
		Status:  &paused,
		IsMuted: &muted,
		Tags:    &noTags,
		Null:    []string{"keyword", "dnsExpectedValue"},
	})
	if err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(rs.gotBody, &sent); err != nil {
		t.Fatalf("corps illisible: %s", rs.gotBody)
	}
	// null explicite = « retirer » ; une clé absente voudrait dire « inchangé ».
	for _, k := range []string{"keyword", "dnsExpectedValue"} {
		v, present := sent[k]
		if !present || v != nil {
			t.Fatalf("%s devrait être envoyé à null: %s", k, rs.gotBody)
		}
	}
	if tags, ok := sent["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("tags devrait être envoyé comme liste vide: %s", rs.gotBody)
	}
	if sent["status"] != "paused" || sent["isMuted"] != true {
		t.Fatalf("status/isMuted: %s", rs.gotBody)
	}
	// Rien d'autre : un champ non renseigné ne part pas.
	if len(sent) != 5 {
		t.Fatalf("champs inattendus dans le corps: %s", rs.gotBody)
	}
}

func TestUpdateMonitorOmitsUnsetFields(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"mon-1"}}`)
	c := New(rs.srv.URL, "k")

	name := "API"
	if _, err := c.UpdateMonitor(context.Background(), "mon-1", MonitorUpdateInput{Name: &name}); err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}
	if string(rs.gotBody) != `{"name":"API"}` {
		t.Fatalf("corps attendu {\"name\":\"API\"}, reçu %s", rs.gotBody)
	}
}

func TestGetMonitorDecodesSharedSettings(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"mon-1","status":"paused","tags":["prod"],"isMuted":true,
		"keyword":"ok","followRedirects":false,"port":8443,"dnsRecordType":"TXT","dnsExpectedValue":null}}`)
	c := New(rs.srv.URL, "k")

	m, err := c.GetMonitor(context.Background(), "mon-1")
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if len(m.Tags) != 1 || m.Tags[0] != "prod" || m.IsMuted == nil || !*m.IsMuted {
		t.Fatalf("tags/isMuted: %+v", m)
	}
	if m.Keyword == nil || *m.Keyword != "ok" || m.FollowRedirects == nil || *m.FollowRedirects {
		t.Fatalf("keyword/followRedirects: %+v", m)
	}
	if m.Port == nil || *m.Port != 8443 || m.DNSRecordType == nil || *m.DNSRecordType != "TXT" || m.DNSExpectedValue != nil {
		t.Fatalf("port/dns: %+v", m)
	}
}

func TestSetMonitorAlertChannelsSendsEmptyListNotNull(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"channelIds":[]}}`)
	c := New(rs.srv.URL, "k")

	kept, err := c.SetMonitorAlertChannels(context.Background(), "mon-1", nil)
	if err != nil {
		t.Fatalf("SetMonitorAlertChannels: %v", err)
	}
	if len(kept) != 0 {
		t.Fatalf("aucun canal attendu: %v", kept)
	}
	if rs.gotMethod != http.MethodPut || rs.gotPath != "/monitors/mon-1/alert-channels" {
		t.Fatalf("méthode/chemin: %s %s", rs.gotMethod, rs.gotPath)
	}
	// `null` serait refusé par l'API (un tableau est attendu).
	if string(rs.gotBody) != `{"channelIds":[]}` {
		t.Fatalf("corps: %s", rs.gotBody)
	}
}

func TestGetMonitorAlertChannels(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"channelIds":["ch-1","ch-2"]}}`)
	c := New(rs.srv.URL, "k")

	ids, err := c.GetMonitorAlertChannels(context.Background(), "mon-1")
	if err != nil || len(ids) != 2 || ids[1] != "ch-2" {
		t.Fatalf("GetMonitorAlertChannels: %v %v", ids, err)
	}
}

func TestMonitorGroupUpdateSendsMembershipOnlyWhenAsked(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"g-1","name":"Payments","monitorIds":["mon-1"]}}`)
	c := New(rs.srv.URL, "k")

	if _, err := c.UpdateMonitorGroup(context.Background(), "g-1", MonitorGroupUpdateInput{Name: "Payments"}); err != nil {
		t.Fatalf("UpdateMonitorGroup: %v", err)
	}
	var sent map[string]any
	_ = json.Unmarshal(rs.gotBody, &sent)
	if _, present := sent["monitorIds"]; present {
		t.Fatalf("monitorIds ne doit pas partir quand l'appartenance ne change pas: %s", rs.gotBody)
	}
	// Description vide et couleur nulle effacent la valeur stockée ; les
	// listes partent vides, jamais à null.
	if sent["description"] != "" || sent["color"] != nil {
		t.Fatalf("description/color: %s", rs.gotBody)
	}
	if tags, ok := sent["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("tags: %s", rs.gotBody)
	}

	none := []string{}
	if _, err := c.UpdateMonitorGroup(context.Background(), "g-1", MonitorGroupUpdateInput{Name: "Payments", MonitorIDs: &none}); err != nil {
		t.Fatalf("UpdateMonitorGroup: %v", err)
	}
	_ = json.Unmarshal(rs.gotBody, &sent)
	if ids, ok := sent["monitorIds"].([]any); !ok || len(ids) != 0 {
		t.Fatalf("vider le groupe = monitorIds: [] : %s", rs.gotBody)
	}
}

func TestMonitorGroupCRUDPaths(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":{"id":"g-1","name":"Payments","tags":["prod"],"alertChannelIds":["ch-1"],"monitorIds":["mon-1"]}}`)
	c := New(rs.srv.URL, "k")
	ctx := context.Background()

	g, err := c.CreateMonitorGroup(ctx, MonitorGroupCreateInput{Name: "Payments", MonitorIDs: []string{"mon-1"}})
	if err != nil || g.ID != "g-1" || len(g.MonitorIDs) != 1 {
		t.Fatalf("CreateMonitorGroup: %+v %v", g, err)
	}
	if rs.gotMethod != http.MethodPost || rs.gotPath != "/monitor-groups" {
		t.Fatalf("create: %s %s", rs.gotMethod, rs.gotPath)
	}
	if _, err := c.GetMonitorGroup(ctx, "g-1"); err != nil || rs.gotPath != "/monitor-groups/g-1" {
		t.Fatalf("get: %s %v", rs.gotPath, err)
	}
	if err := c.DeleteMonitorGroup(ctx, "g-1"); err != nil || rs.gotMethod != http.MethodDelete {
		t.Fatalf("delete: %s %v", rs.gotMethod, err)
	}
}

func TestListPrivateAgents(t *testing.T) {
	rs := newServer(t, http.StatusOK, `{"data":[{"id":"a-1","name":"dc-paris","site":null,"region":"private:a-1","status":"up"}],
		"meta":{"eligible":true,"limit":10,"latestVersion":"1.0.0"}}`)
	c := New(rs.srv.URL, "k")

	list, err := c.ListPrivateAgents(context.Background())
	if err != nil {
		t.Fatalf("ListPrivateAgents: %v", err)
	}
	if len(list.Agents) != 1 || list.Agents[0].Region != "private:a-1" || list.Agents[0].Site != nil {
		t.Fatalf("agents: %+v", list.Agents)
	}
	if !list.Meta.Eligible || list.Meta.Limit != 10 || list.Meta.LatestVersion == nil {
		t.Fatalf("meta: %+v", list.Meta)
	}
}
