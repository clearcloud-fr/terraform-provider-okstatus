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
	"strconv"
	"strings"
	"time"
)

const DefaultEndpoint = "https://api.okstatus.eu/api/v1"

// Client is a small wrapper around http.Client that adds the Bearer API key
// and JSON encode/decode boilerplate shared by every resource.
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
	// sleep waits between attempts; replaced in tests.
	sleep func(context.Context, time.Duration) error
}

// A refresh reads every resource, and the API allows a limited number of
// requests per minute: with a few dozen monitors a plain `terraform plan`
// runs into 429. The client waits and retries instead of failing the run.
const (
	maxAttempts  = 8
	maxRetryWait = 65 * time.Second
)

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryWait says how long to wait before retrying a response, or false when
// the response is final. Retried: 429, and the 502/503 "UNAVAILABLE" that
// APIs older than 2026-10-11 answered in place of a 429.
func retryWait(status int, code string, retryAfter string, attempt int) (time.Duration, bool) {
	throttled := status == http.StatusTooManyRequests
	unavailable := (status == http.StatusBadGateway || status == http.StatusServiceUnavailable) && code == "UNAVAILABLE"
	if !throttled && !unavailable {
		return 0, false
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
		return min(time.Duration(secs)*time.Second, maxRetryWait), true
	}
	// 2s, 4s, 8s, 16s, 32s, then capped.
	return min(time.Duration(2<<uint(attempt))*time.Second, maxRetryWait), true
}

func New(endpoint, apiKey string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	// Trailing slash on a user-supplied endpoint would double up against the
	// leading slash of every path (".../api/v1//monitors"). Trim it once here.
	endpoint = strings.TrimRight(endpoint, "/")
	return &Client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		sleep:      sleepContext,
	}
}

// APIError mirrors api-v1's uniform error shape: { "error": { "code", "message" } }
// (see the OKS-60 error-format-normalization fix — every api-v1 route uses this).
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	// RetryAfter is the response's Retry-After header (seconds), if any.
	RetryAfter string
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
	var encoded []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		encoded = b
	}

	for attempt := 0; ; attempt++ {
		err := c.doOnce(ctx, method, path, encoded, out)
		apiErr, isAPIErr := err.(*APIError)
		if !isAPIErr || attempt+1 >= maxAttempts {
			return err
		}
		wait, retry := retryWait(apiErr.StatusCode, apiErr.Code, apiErr.RetryAfter, attempt)
		if !retry {
			return err
		}
		if sleepErr := c.sleep(ctx, wait); sleepErr != nil {
			return err
		}
	}
}

// doOnce sends one request. A throttled request was refused before reaching
// any handler, so sending it again is safe for every method.
func (c *Client) doOnce(ctx context.Context, method, path string, encoded []byte, out any) error {
	var reqBody io.Reader
	body := encoded
	if body != nil {
		reqBody = bytes.NewReader(body)
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
		apiErr := &APIError{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
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
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Type                string     `json:"type"`
	Interval            int64      `json:"interval"`
	Regions             []string   `json:"regions"`
	AcceptedStatusCodes []string   `json:"acceptedStatusCodes"`
	Status              string     `json:"status"`
	LlmProvider         string     `json:"llmProvider,omitempty"`
	LlmConfig           *LlmConfig `json:"llmConfig,omitempty"`
	// Settings shared with the dashboard and the MCP connector (v1.6). Each
	// is nullable server-side except tags.
	Tags             []string `json:"tags"`
	IsMuted          *bool    `json:"isMuted"`
	Keyword          *string  `json:"keyword"`
	FollowRedirects  *bool    `json:"followRedirects"`
	Port             *int64   `json:"port"`
	DNSRecordType    *string  `json:"dnsRecordType"`
	DNSExpectedValue *string  `json:"dnsExpectedValue"`
	// HTTP request options. httpHeaders is deliberately absent: the API
	// returns it masked ("***"), so there is nothing to read back.
	HTTPMethod       *string `json:"httpMethod"`
	HTTPBody         *string `json:"httpBody"`
	HTTPBodyEncoding *string `json:"httpBodyEncoding"`
	IgnoreTLS        *bool   `json:"ignoreTls"`
	// Assertions and per-type settings. basicAuthUser/Pass and authConfig
	// are absent for the same reason as httpHeaders: they come back masked.
	Retries           *int64   `json:"retries"`
	Severity3xx       *string  `json:"severity3xx"`
	KeywordInvert     *bool    `json:"keywordInvert"`
	JSONPath          *string  `json:"jsonPath"`
	JSONOperator      *string  `json:"jsonOperator"`
	JSONExpectedValue *string  `json:"jsonExpectedValue"`
	DNSServer         *string  `json:"dnsServer"`
	DBQuery           *string  `json:"dbQuery"`
	GracePeriod       *int64   `json:"gracePeriod"`
	ChildMonitorIDs   []string `json:"childMonitorIds"`
	PushToken         *string  `json:"pushToken"`
}

// ─── LLM monitors (OKS-266) ────────────────────────────────────
// Mirrors ok-status-backend's llmConfigSchema (src/api/schemas/monitors.ts).
// llmApiKey is write-only: reads come back masked as "***" (sanitize-monitor),
// so the resource never round-trips it through the API.

type LlmTimeouts struct {
	ConnectMs    int64 `json:"connectMs"`
	FirstTokenMs int64 `json:"firstTokenMs"`
	TotalMs      int64 `json:"totalMs"`
}

type LlmAssertions struct {
	TtftMsMax     int64  `json:"ttftMsMax"`
	ContentEquals string `json:"contentEquals,omitempty"`
	ContentRegex  string `json:"contentRegex,omitempty"`
}

type LlmConfig struct {
	Model      string        `json:"model"`
	Prompt     string        `json:"prompt"`
	Region     string        `json:"region,omitempty"`
	Timeouts   LlmTimeouts   `json:"timeouts"`
	Assertions LlmAssertions `json:"assertions"`
}

type monitorEnvelope struct {
	Data Monitor `json:"data"`
}

type MonitorCreateInput struct {
	Name                string     `json:"name"`
	URL                 string     `json:"url"`
	Type                string     `json:"type,omitempty"`
	Interval            int64      `json:"interval,omitempty"`
	Regions             []string   `json:"regions,omitempty"`
	AcceptedStatusCodes []string   `json:"acceptedStatusCodes,omitempty"`
	LlmProvider         string     `json:"llmProvider,omitempty"`
	LlmAPIKey           string     `json:"llmApiKey,omitempty"`
	LlmConfig           *LlmConfig `json:"llmConfig,omitempty"`
	Tags                []string   `json:"tags,omitempty"`
	Keyword             string     `json:"keyword,omitempty"`
	FollowRedirects     *bool      `json:"followRedirects,omitempty"`
	Port                *int64     `json:"port,omitempty"`
	DNSRecordType       string     `json:"dnsRecordType,omitempty"`
	DNSExpectedValue    string     `json:"dnsExpectedValue,omitempty"`
	HTTPMethod          string     `json:"httpMethod,omitempty"`
	HTTPBody            string     `json:"httpBody,omitempty"`
	HTTPBodyEncoding    string     `json:"httpBodyEncoding,omitempty"`
	// nil = not sent; an empty map is not sent either (nothing to store).
	HTTPHeaders map[string]string `json:"httpHeaders,omitempty"`
	IgnoreTLS   *bool             `json:"ignoreTls,omitempty"`

	Retries           *int64   `json:"retries,omitempty"`
	Severity3xx       string   `json:"severity3xx,omitempty"`
	KeywordInvert     *bool    `json:"keywordInvert,omitempty"`
	JSONPath          string   `json:"jsonPath,omitempty"`
	JSONOperator      string   `json:"jsonOperator,omitempty"`
	JSONExpectedValue string   `json:"jsonExpectedValue,omitempty"`
	DNSServer         string   `json:"dnsServer,omitempty"`
	DBQuery           string   `json:"dbQuery,omitempty"`
	GracePeriod       *int64   `json:"gracePeriod,omitempty"`
	ChildMonitorIDs   []string `json:"childMonitorIds,omitempty"`
	BasicAuthUser     string   `json:"basicAuthUser,omitempty"`
	BasicAuthPass     string   `json:"basicAuthPass,omitempty"`
	// AuthConfig uses the API's own field names (type, token, tokenUrl, …).
	AuthConfig map[string]string `json:"authConfig,omitempty"`
}

type MonitorUpdateInput struct {
	Name                *string    `json:"name,omitempty"`
	URL                 *string    `json:"url,omitempty"`
	Interval            *int64     `json:"interval,omitempty"`
	Regions             []string   `json:"regions,omitempty"`
	AcceptedStatusCodes []string   `json:"acceptedStatusCodes,omitempty"`
	LlmProvider         string     `json:"llmProvider,omitempty"`
	LlmAPIKey           string     `json:"llmApiKey,omitempty"`
	LlmConfig           *LlmConfig `json:"llmConfig,omitempty"`
	// A pointer to an empty slice is sent as [] (removes every tag); nil
	// leaves the tags untouched.
	Tags *[]string `json:"tags,omitempty"`
	// "active" or "paused".
	Status           *string `json:"status,omitempty"`
	IsMuted          *bool   `json:"isMuted,omitempty"`
	Keyword          *string `json:"keyword,omitempty"`
	FollowRedirects  *bool   `json:"followRedirects,omitempty"`
	Port             *int64  `json:"port,omitempty"`
	DNSRecordType    *string `json:"dnsRecordType,omitempty"`
	DNSExpectedValue *string `json:"dnsExpectedValue,omitempty"`
	HTTPMethod       *string `json:"httpMethod,omitempty"`
	HTTPBody         *string `json:"httpBody,omitempty"`
	HTTPBodyEncoding *string `json:"httpBodyEncoding,omitempty"`
	// A pointer to an empty map is sent as {} (removes every header); nil
	// leaves the stored headers untouched.
	HTTPHeaders *map[string]string `json:"httpHeaders,omitempty"`
	IgnoreTLS   *bool              `json:"ignoreTls,omitempty"`

	Retries           *int64   `json:"retries,omitempty"`
	Severity3xx       *string  `json:"severity3xx,omitempty"`
	KeywordInvert     *bool    `json:"keywordInvert,omitempty"`
	JSONPath          *string  `json:"jsonPath,omitempty"`
	JSONOperator      *string  `json:"jsonOperator,omitempty"`
	JSONExpectedValue *string  `json:"jsonExpectedValue,omitempty"`
	DNSServer         *string  `json:"dnsServer,omitempty"`
	DBQuery           *string  `json:"dbQuery,omitempty"`
	ChildMonitorIDs   []string `json:"childMonitorIds,omitempty"`
	// Credentials: a value replaces the stored one; removing one is an
	// explicit null (see Null).
	BasicAuthUser *string           `json:"basicAuthUser,omitempty"`
	BasicAuthPass *string           `json:"basicAuthPass,omitempty"`
	AuthConfig    map[string]string `json:"authConfig,omitempty"`
	// Null lists JSON field names to send as an explicit null — how the API
	// removes a keyword or a DNS expectation ("omitted" means "unchanged").
	Null []string `json:"-"`
}

// MarshalJSON adds the explicit nulls named in Null to the regular encoding.
func (in MonitorUpdateInput) MarshalJSON() ([]byte, error) {
	type plain MonitorUpdateInput
	b, err := json.Marshal(plain(in))
	if err != nil || len(in.Null) == 0 {
		return b, err
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil, err
	}
	for _, name := range in.Null {
		fields[name] = nil
	}
	return json.Marshal(fields)
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

type monitorListEnvelope struct {
	Monitors []Monitor `json:"monitors"`
	Pages    int64     `json:"pages"`
}

// ListMonitors returns every monitor in the org, paging through GET
// /monitors internally — a single page caps at 50 (PageQuerySchema in
// api-v1.ts), and a caller asking "what already exists" wants the full set,
// not a silently truncated first page.
func (c *Client) ListMonitors(ctx context.Context) ([]Monitor, error) {
	const perPage = 100
	var all []Monitor
	for page := int64(1); ; page++ {
		var env monitorListEnvelope
		path := fmt.Sprintf("/monitors?page=%d&per_page=%d", page, perPage)
		if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
			return nil, err
		}
		all = append(all, env.Monitors...)
		if len(env.Monitors) == 0 || page >= env.Pages {
			break
		}
	}
	return all, nil
}

// ─── Alert channels of a monitor ────────────────────────────────

type monitorAlertChannelsEnvelope struct {
	Data struct {
		ChannelIDs []string `json:"channelIds"`
	} `json:"data"`
}

func (c *Client) GetMonitorAlertChannels(ctx context.Context, monitorID string) ([]string, error) {
	var env monitorAlertChannelsEnvelope
	if err := c.do(ctx, http.MethodGet, "/monitors/"+monitorID+"/alert-channels", nil, &env); err != nil {
		return nil, err
	}
	return env.Data.ChannelIDs, nil
}

// SetMonitorAlertChannels replaces the monitor's set of attached channels;
// an empty list detaches them all. An id that is not one of the
// organization's channels is refused (404) before anything is changed.
func (c *Client) SetMonitorAlertChannels(ctx context.Context, monitorID string, channelIDs []string) ([]string, error) {
	if channelIDs == nil {
		channelIDs = []string{}
	}
	body := struct {
		ChannelIDs []string `json:"channelIds"`
	}{channelIDs}
	var env monitorAlertChannelsEnvelope
	if err := c.do(ctx, http.MethodPut, "/monitors/"+monitorID+"/alert-channels", body, &env); err != nil {
		return nil, err
	}
	return env.Data.ChannelIDs, nil
}

// ─── Monitor groups ─────────────────────────────────────────────

type MonitorGroup struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     *string  `json:"description"`
	Color           *string  `json:"color"`
	Tags            []string `json:"tags"`
	AlertChannelIDs []string `json:"alertChannelIds"`
	MonitorIDs      []string `json:"monitorIds"`
	Paused          bool     `json:"paused"`
	Muted           bool     `json:"muted"`
}

type monitorGroupEnvelope struct {
	Data MonitorGroup `json:"data"`
}

type MonitorGroupCreateInput struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Color           *string  `json:"color,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	AlertChannelIDs []string `json:"alertChannelIds,omitempty"`
	MonitorIDs      []string `json:"monitorIds,omitempty"`
}

// MonitorGroupUpdateInput always sends the descriptive fields (the resource
// owns them): Description "" and Color nil clear the stored value. MonitorIDs
// is only sent when non-nil, since replacing the membership is a separate
// write server-side.
type MonitorGroupUpdateInput struct {
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Color           *string   `json:"color"`
	Tags            []string  `json:"tags"`
	AlertChannelIDs []string  `json:"alertChannelIds"`
	MonitorIDs      *[]string `json:"monitorIds,omitempty"`
}

func (c *Client) ListMonitorGroups(ctx context.Context) ([]MonitorGroup, error) {
	var env struct {
		Data []MonitorGroup `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/monitor-groups", nil, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func (c *Client) CreateMonitorGroup(ctx context.Context, in MonitorGroupCreateInput) (*MonitorGroup, error) {
	var env monitorGroupEnvelope
	if err := c.do(ctx, http.MethodPost, "/monitor-groups", in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) GetMonitorGroup(ctx context.Context, id string) (*MonitorGroup, error) {
	var env monitorGroupEnvelope
	if err := c.do(ctx, http.MethodGet, "/monitor-groups/"+id, nil, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

func (c *Client) UpdateMonitorGroup(ctx context.Context, id string, in MonitorGroupUpdateInput) (*MonitorGroup, error) {
	if in.Tags == nil {
		in.Tags = []string{}
	}
	if in.AlertChannelIDs == nil {
		in.AlertChannelIDs = []string{}
	}
	var env monitorGroupEnvelope
	if err := c.do(ctx, http.MethodPatch, "/monitor-groups/"+id, in, &env); err != nil {
		return nil, err
	}
	return &env.Data, nil
}

// DeleteMonitorGroup removes the group only: its monitors are kept, ungrouped.
func (c *Client) DeleteMonitorGroup(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/monitor-groups/"+id, nil, nil)
}

// ─── Private agents (read-only) ─────────────────────────────────

type PrivateAgent struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Site *string `json:"site"`
	// Region is the value to put in a monitor's regions to run it on this
	// agent ("private:<id>").
	Region          string  `json:"region"`
	Status          string  `json:"status"`
	Suspended       bool    `json:"suspended"`
	Version         *string `json:"version"`
	Hostname        *string `json:"hostname"`
	Platform        *string `json:"platform"`
	LastHeartbeatAt *string `json:"lastHeartbeatAt"`
}

type PrivateAgentList struct {
	Agents []PrivateAgent `json:"data"`
	Meta   struct {
		Eligible      bool    `json:"eligible"`
		Limit         int64   `json:"limit"`
		LatestVersion *string `json:"latestVersion"`
	} `json:"meta"`
}

func (c *Client) ListPrivateAgents(ctx context.Context) (*PrivateAgentList, error) {
	var out PrivateAgentList
	if err := c.do(ctx, http.MethodGet, "/private-agents", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
	ID   string  `json:"id"`
	Type string  `json:"type"`
	Name *string `json:"name"`
	// Config values are not all strings (a Jira channel carries the boolean
	// `autoResolve`): decoding them as map[string]string made the whole
	// list — and so every channel's Read — fail. Secrets come back masked.
	Config  map[string]any `json:"config"`
	Enabled bool           `json:"enabled"`
}

// ConfigStrings is Config with every value as the string Terraform holds
// ("true" for a boolean, the plain number for a number).
func (ch AlertChannel) ConfigStrings() map[string]string {
	out := make(map[string]string, len(ch.Config))
	for k, v := range ch.Config {
		switch t := v.(type) {
		case string:
			out[k] = t
		case nil:
			// absent
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

type AlertChannelCreateInput struct {
	Type    string         `json:"type"`
	Config  map[string]any `json:"config"`
	Enabled *bool          `json:"enabled,omitempty"`
}

// ChannelConfigValues turns Terraform's string map into the API's config:
// "true" / "false" become booleans, which is what the API expects for the
// few boolean settings; everything else stays a string.
func ChannelConfigValues(config map[string]string) map[string]any {
	out := make(map[string]any, len(config))
	for k, v := range config {
		switch v {
		case "true":
			out[k] = true
		case "false":
			out[k] = false
		default:
			out[k] = v
		}
	}
	return out
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

// ListAlertChannels returns every alert channel in the org — api-v1 has no
// pagination on this endpoint, it's always the full set in one response.
func (c *Client) ListAlertChannels(ctx context.Context) ([]AlertChannel, error) {
	var env struct {
		Channels []AlertChannel `json:"channels"`
	}
	if err := c.do(ctx, http.MethodGet, "/alert-channels", nil, &env); err != nil {
		return nil, err
	}
	return env.Channels, nil
}

// GetAlertChannel — api-v1 has no GET /alert-channels/:id, only the list;
// filter it client-side (mirrors what a hand-written Terraform Read for a
// list-only backend resource has to do).
func (c *Client) GetAlertChannel(ctx context.Context, id string) (*AlertChannel, error) {
	channels, err := c.ListAlertChannels(ctx)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
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
