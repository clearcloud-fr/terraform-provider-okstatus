// Package client is a thin HTTP client for OKStatus's public api-v1
// (see ok-status/apps/api/src/routes/api-v1.ts — this package mirrors that
// file's request/response shapes field-for-field).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultEndpoint = "https://api.okstatus.eu/api/v1"

// Client is a small wrapper around http.Client that adds the Bearer API key
// and JSON encode/decode boilerplate shared by every resource.
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

func New(endpoint, apiKey string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError mirrors api-v1's uniform error shape: { "error": { "code", "message" } }
// (see the OKS-60 error-format-normalization fix — every api-v1 route uses this).
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code == "" && e.Message == "" {
		return fmt.Sprintf("okstatus api: unexpected status %d", e.StatusCode)
	}
	return fmt.Sprintf("okstatus api: %s (%s, HTTP %d)", e.Message, e.Code, e.StatusCode)
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends a request and decodes a successful (< 400) JSON response into out.
// out may be nil for responses with no body worth decoding (e.g. delete).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling okstatus api: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var env errorEnvelope
		if json.Unmarshal(respBody, &env) == nil {
			apiErr.Code = env.Error.Code
			apiErr.Message = env.Error.Message
		}
		return apiErr
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decoding response body: %w", err)
	}
	return nil
}

// IsNotFound reports whether err is an APIError for a 404 response — every
// resource's Read() uses this to know when to remove an item from state.
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == 404
}

// ─── Monitors ───────────────────────────────────────────────────

type Monitor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	Interval int64  `json:"interval"`
	Status   string `json:"status"`
}

type monitorEnvelope struct {
	Data Monitor `json:"data"`
}

type MonitorCreateInput struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Type     string `json:"type,omitempty"`
	Interval int64  `json:"interval,omitempty"`
}

type MonitorUpdateInput struct {
	Name     *string `json:"name,omitempty"`
	URL      *string `json:"url,omitempty"`
	Interval *int64  `json:"interval,omitempty"`
}

func (c *Client) CreateMonitor(ctx context.Context, in MonitorCreateInput) (*Monitor, error) {
	var env monitorEnvelope
	if err := c.do(ctx, http.MethodPost, "/monitors", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetMonitor(ctx context.Context, id string) (*Monitor, error) {
	var env monitorEnvelope
	if err := c.do(ctx, http.MethodGet, "/monitors/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateMonitor(ctx context.Context, id string, in MonitorUpdateInput) (*Monitor, error) {
	var env monitorEnvelope
	if err := c.do(ctx, http.MethodPatch, "/monitors/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) DeleteMonitor(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/monitors/"+id, nil, nil)
}

// ─── Incidents ──────────────────────────────────────────────────

type Incident struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	Severity       string  `json:"severity"`
	MonitorID      string  `json:"monitorId"`
	OrganizationID string  `json:"organizationId"`
	ResolvedAt     *string `json:"resolvedAt"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

type incidentEnvelope struct {
	Data Incident `json:"data"`
}

type IncidentCreateInput struct {
	Title     string `json:"title"`
	Severity  string `json:"severity,omitempty"`
	MonitorID string `json:"monitorId"`
}

// IncidentStatusUpdateInput mirrors PATCH /incidents/:id, which appends a
// timeline update and moves the incident's status — it isn't a general
// field patch (title/severity/monitor_id can't be changed after creation).
type IncidentStatusUpdateInput struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (c *Client) CreateIncident(ctx context.Context, in IncidentCreateInput) (*Incident, error) {
	var env incidentEnvelope
	if err := c.do(ctx, http.MethodPost, "/incidents", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetIncident(ctx context.Context, id string) (*Incident, error) {
	var env struct {
		Data Incident `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/incidents/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateIncidentStatus(ctx context.Context, id string, in IncidentStatusUpdateInput) (*Incident, error) {
	var env incidentEnvelope
	if err := c.do(ctx, http.MethodPatch, "/incidents/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// ─── Status Pages ───────────────────────────────────────────────

type StatusPage struct {
	ID                  string  `json:"id"`
	Slug                string  `json:"slug"`
	Title               string  `json:"title"`
	Description         *string `json:"description"`
	LogoURL             *string `json:"logoUrl"`
	FaviconURL          *string `json:"faviconUrl"`
	AccentColor         string  `json:"accentColor"`
	Theme               string  `json:"theme"`
	CustomCSS           *string `json:"customCss"`
	FooterText          *string `json:"footerText"`
	Published           bool    `json:"published"`
	AutoRefreshInterval int64   `json:"autoRefreshInterval"`
	CustomDomain        *string `json:"customDomain"`
}

type StatusPageCreateInput struct {
	Slug        string  `json:"slug"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	LogoURL     *string `json:"logoUrl,omitempty"`
	FaviconURL  *string `json:"faviconUrl,omitempty"`
	AccentColor *string `json:"accentColor,omitempty"`
	Theme       *string `json:"theme,omitempty"`
	CustomCSS   *string `json:"customCss,omitempty"`
	FooterText  *string `json:"footerText,omitempty"`
	Published   *bool   `json:"published,omitempty"`
}

type StatusPageUpdateInput struct {
	Title               *string `json:"title,omitempty"`
	Description         *string `json:"description,omitempty"`
	LogoURL             *string `json:"logoUrl,omitempty"`
	FaviconURL          *string `json:"faviconUrl,omitempty"`
	AccentColor         *string `json:"accentColor,omitempty"`
	Theme               *string `json:"theme,omitempty"`
	CustomCSS           *string `json:"customCss,omitempty"`
	FooterText          *string `json:"footerText,omitempty"`
	Published           *bool   `json:"published,omitempty"`
	AutoRefreshInterval *int64  `json:"autoRefreshInterval,omitempty"`
	CustomDomain        *string `json:"customDomain,omitempty"`
}

func (c *Client) CreateStatusPage(ctx context.Context, in StatusPageCreateInput) (*StatusPage, error) {
	var out StatusPage
	if err := c.do(ctx, http.MethodPost, "/status-pages", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetStatusPage(ctx context.Context, id string) (*StatusPage, error) {
	var env struct {
		StatusPage StatusPage `json:"statusPage"`
	}
	if err := c.do(ctx, http.MethodGet, "/status-pages/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.StatusPage, nil
}

func (c *Client) UpdateStatusPage(ctx context.Context, id string, in StatusPageUpdateInput) (*StatusPage, error) {
	var out StatusPage
	if err := c.do(ctx, http.MethodPatch, "/status-pages/"+id, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteStatusPage(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/status-pages/"+id, nil, nil)
}

// ─── Alert Channels ─────────────────────────────────────────────

type AlertChannel struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Config  map[string]string `json:"config"`
	Enabled bool              `json:"enabled"`
}

type AlertChannelCreateInput struct {
	Type    string            `json:"type"`
	Config  map[string]string `json:"config"`
	Enabled *bool             `json:"enabled,omitempty"`
}

type AlertChannelUpdateInput struct {
	Enabled bool `json:"enabled"`
}

func (c *Client) CreateAlertChannel(ctx context.Context, in AlertChannelCreateInput) (*AlertChannel, error) {
	var out AlertChannel
	if err := c.do(ctx, http.MethodPost, "/alert-channels", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAlertChannel — api-v1 has no GET /alert-channels/:id, only the list;
// filter it client-side (mirrors what a hand-written Terraform Read for a
// list-only backend resource has to do).
func (c *Client) GetAlertChannel(ctx context.Context, id string) (*AlertChannel, error) {
	var env struct {
		Channels []AlertChannel `json:"channels"`
	}
	if err := c.do(ctx, http.MethodGet, "/alert-channels", nil, &env); err != nil {
		return nil, err
	}
	for _, ch := range env.Channels {
		if ch.ID == id {
			return &ch, nil
		}
	}
	return nil, &APIError{StatusCode: 404, Code: "NOT_FOUND", Message: "Alert channel not found"}
}

func (c *Client) UpdateAlertChannel(ctx context.Context, id string, in AlertChannelUpdateInput) (*AlertChannel, error) {
	var out AlertChannel
	if err := c.do(ctx, http.MethodPatch, "/alert-channels/"+id, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteAlertChannel(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/alert-channels/"+id, nil, nil)
}

// ─── Maintenances ───────────────────────────────────────────────

type Maintenance struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Message        *string  `json:"message"`
	Status         string   `json:"status"`
	ScheduledStart string   `json:"scheduledStart"`
	ScheduledEnd   string   `json:"scheduledEnd"`
	ActualEnd      *string  `json:"actualEnd"`
	OrganizationID string   `json:"organizationId"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	MonitorIDs     []string `json:"monitorIds"`
}

type maintenanceEnvelope struct {
	Maintenance Maintenance `json:"maintenance"`
}

type MaintenanceCreateInput struct {
	Title          string   `json:"title"`
	Message        *string  `json:"message,omitempty"`
	ScheduledStart string   `json:"scheduledStart"`
	ScheduledEnd   string   `json:"scheduledEnd"`
	MonitorIDs     []string `json:"monitorIds"`
}

type MaintenanceUpdateInput struct {
	Title          *string  `json:"title,omitempty"`
	Message        *string  `json:"message,omitempty"`
	ScheduledStart *string  `json:"scheduledStart,omitempty"`
	ScheduledEnd   *string  `json:"scheduledEnd,omitempty"`
	MonitorIDs     []string `json:"monitorIds,omitempty"`
	Status         *string  `json:"status,omitempty"`
}

func (c *Client) CreateMaintenance(ctx context.Context, in MaintenanceCreateInput) (*Maintenance, error) {
	var env maintenanceEnvelope
	if err := c.do(ctx, http.MethodPost, "/maintenances", in, &env); err != nil {
		return nil, err
	}
	return &env.Maintenance, nil
}

func (c *Client) GetMaintenance(ctx context.Context, id string) (*Maintenance, error) {
	var env maintenanceEnvelope
	if err := c.do(ctx, http.MethodGet, "/maintenances/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Maintenance, nil
}

func (c *Client) UpdateMaintenance(ctx context.Context, id string, in MaintenanceUpdateInput) (*Maintenance, error) {
	var env maintenanceEnvelope
	if err := c.do(ctx, http.MethodPatch, "/maintenances/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Maintenance, nil
}

func (c *Client) DeleteMaintenance(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/maintenances/"+id, nil, nil)
}

// ─── Org (read-only) ────────────────────────────────────────────

type Org struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Plan      string `json:"plan"`
	Timezone  string `json:"timezone"`
	CreatedAt string `json:"createdAt"`
}

func (c *Client) GetOrg(ctx context.Context) (*Org, error) {
	var env struct {
		Data Org `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/org", nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}
