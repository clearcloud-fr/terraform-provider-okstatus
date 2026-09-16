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
