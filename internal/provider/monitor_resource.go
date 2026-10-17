package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var statusCodeRegexp = regexp.MustCompile(statusCodePattern)

// One private agent, as the `region` attribute of data.okstatus_private_agents
// gives it. The id is dynamic, so it cannot be part of validRegionCodes.
var privateAgentRegionRegexp = regexp.MustCompile(`^private:[0-9a-fA-F-]{36}$`)

// regionValidator accepts a public region code, "private" (any of the
// organization's agents) or "private:<agent id>" (that agent only) — the
// three forms ok-status-backend's monitorRegionSchema accepts.
func regionValidator() validator.String {
	return stringvalidator.Any(
		stringvalidator.OneOf(validRegionCodes...),
		stringvalidator.RegexMatches(privateAgentRegionRegexp, "must be a region code, \"private\" or \"private:<agent id>\""),
	)
}

// Every check type of the API except "llm", which has its own resource
// (okstatus_llm_monitor).
var monitorTypes = []string{
	"http", "tcp", "ping", "dns", "grpc", "push", "group",
	"postgres", "mysql", "mongodb", "smtp", "imap", "redis",
}

var jsonOperators = []string{"==", "!=", ">", "<", ">=", "<=", "contains"}

// urlCredentialsRegexp spots "scheme://user:password@host". OKStatus moves
// such credentials out of the URL into its encrypted store and returns the
// URL without them, which Terraform would report as an inconsistent result.
var urlCredentialsRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://[^/?#]*@`)

type noURLCredentialsValidator struct{}

func (noURLCredentialsValidator) Description(context.Context) string {
	return "must not contain credentials (user:password@)"
}

func (v noURLCredentialsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (noURLCredentialsValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if urlCredentialsRegexp.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Credentials in the target",
			"Put the user and password in basic_auth_user / basic_auth_password, not in the URL: "+
				"OKStatus stores them apart from the target and returns the URL without them.")
	}
}

// monitorAuthModel is the `auth` block. Field names map to the API's
// authConfig (authFieldNames); which ones apply depends on `type`.
type monitorAuthModel struct {
	Type         types.String `tfsdk:"type"`
	Token        types.String `tfsdk:"token"`
	In           types.String `tfsdk:"in"`
	Name         types.String `tfsdk:"name"`
	Value        types.String `tfsdk:"value"`
	Cookie       types.String `tfsdk:"cookie"`
	TokenURL     types.String `tfsdk:"token_url"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Scope        types.String `tfsdk:"scope"`
	Audience     types.String `tfsdk:"audience"`
	AuthStyle    types.String `tfsdk:"auth_style"`
	Cert         types.String `tfsdk:"cert"`
	Key          types.String `tfsdk:"key"`
	CA           types.String `tfsdk:"ca"`
}

// authConfigFrom turns the `auth` block into the API's authConfig object,
// keeping only the attributes that were written.
func authConfigFrom(ctx context.Context, auth types.Object) (map[string]string, diag.Diagnostics) {
	var m monitorAuthModel
	diags := auth.As(ctx, &m, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return nil, diags
	}
	out := map[string]string{}
	for name, v := range map[string]types.String{
		"type": m.Type, "token": m.Token, "in": m.In, "name": m.Name, "value": m.Value,
		"cookie": m.Cookie, "tokenUrl": m.TokenURL, "clientId": m.ClientID,
		"clientSecret": m.ClientSecret, "scope": m.Scope, "audience": m.Audience,
		"authStyle": m.AuthStyle, "cert": m.Cert, "key": m.Key, "ca": m.CA,
	} {
		if known(v) {
			out[name] = v.ValueString()
		}
	}
	return out, diags
}

var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
var httpBodyEncodings = []string{"json", "form", "xml", "text"}

var dnsRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SOA", "CAA", "SRV"}

var _ resource.Resource = &MonitorResource{}
var _ resource.ResourceWithImportState = &MonitorResource{}

func NewMonitorResource() resource.Resource {
	return &MonitorResource{}
}

type MonitorResource struct {
	client *client.Client
}

type monitorResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	URL                 types.String `tfsdk:"url"`
	Type                types.String `tfsdk:"type"`
	Interval            types.Int64  `tfsdk:"interval"`
	Regions             types.List   `tfsdk:"regions"`
	AcceptedStatusCodes types.List   `tfsdk:"accepted_status_codes"`
	Tags                types.Set    `tfsdk:"tags"`
	Paused              types.Bool   `tfsdk:"paused"`
	Muted               types.Bool   `tfsdk:"muted"`
	Keyword             types.String `tfsdk:"keyword"`
	FollowRedirects     types.Bool   `tfsdk:"follow_redirects"`
	Port                types.Int64  `tfsdk:"port"`
	DNSRecordType       types.String `tfsdk:"dns_record_type"`
	DNSExpectedValue    types.String `tfsdk:"dns_expected_value"`
	HTTPMethod          types.String `tfsdk:"http_method"`
	HTTPHeaders         types.Map    `tfsdk:"http_headers"`
	HTTPBody            types.String `tfsdk:"http_body"`
	HTTPBodyEncoding    types.String `tfsdk:"http_body_encoding"`
	IgnoreTLS           types.Bool   `tfsdk:"ignore_tls"`
	Retries             types.Int64  `tfsdk:"retries"`
	Severity3xx         types.String `tfsdk:"severity_3xx"`
	LatencyThresholdMs  types.Int64  `tfsdk:"latency_threshold_ms"`
	LatencyPublic       types.Bool   `tfsdk:"latency_public"`
	LatencyAnomaly      types.Bool   `tfsdk:"latency_anomaly"`
	KeywordInvert       types.Bool   `tfsdk:"keyword_invert"`
	JSONPath            types.String `tfsdk:"json_path"`
	JSONOperator        types.String `tfsdk:"json_operator"`
	JSONExpectedValue   types.String `tfsdk:"json_expected_value"`
	DNSServer           types.String `tfsdk:"dns_server"`
	DBQuery             types.String `tfsdk:"db_query"`
	GracePeriod         types.Int64  `tfsdk:"grace_period"`
	ChildMonitorIDs     types.Set    `tfsdk:"child_monitor_ids"`
	PushToken           types.String `tfsdk:"push_token"`
	BasicAuthUser       types.String `tfsdk:"basic_auth_user"`
	BasicAuthPassword   types.String `tfsdk:"basic_auth_password"`
	Auth                types.Object `tfsdk:"auth"`
	AlertChannelIDs     types.Set    `tfsdk:"alert_channel_ids"`
	Status              types.String `tfsdk:"status"`
}

// Valid region codes and the accepted-status-code shape ("200" or "200-299")
// mirror ok-status-backend's VALID_REGIONS (src/shared/constants.ts) and
// statusCodePattern (src/api/schemas/monitors.ts) — api-v1 rejects anything
// else with a 400 before this provider ever sees a response.
// OKS-266 — widened from the original 8 codes to the backend's current
// 24-region cc-city set (OKS-482 rename, 2026-08-29), plus "private"
// (customer-deployed private agents, OKS-401). The old bare country codes
// (fr, de, nl, …) stay accepted as dual-accept shims for one release.
var validRegionCodes = []string{
	"fr-dc3", "fr-sbg", "ie-dub", "dk-cph",
	"de-fra", "nl-ams", "es-mad", "se-sto", "pl-waw", "ch-zrh", "uk-lon", "it-mil",
	"ca-bhs", "us-hil", "br-sao", "cl-scl", "sg-sgp", "in-mum", "bh-man", "qa-doh",
	"za-cpt", "za-jnb", "au-syd", "au-mel",
	// deprecated dual-accept shims (renamed to cc-city on 2026-08-29)
	"fr", "de", "nl", "es", "se", "pl", "ch", "uk", "it",
	"ca", "us", "br", "cl", "sg", "in", "bh", "qa",
	// deprecated 2026-10-04: Roubaix → ie-dub, Gravelines → dk-cph (the
	// backend stores the replacement)
	"fr-rbx", "fr-gra",
	"private",
}

const statusCodePattern = `^\d{3}(-\d{3})?$`

func (r *MonitorResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *MonitorResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OKStatus monitor: HTTP, TCP, ping, DNS, gRPC, database (PostgreSQL, MySQL, MongoDB), " +
			"SMTP, IMAP, Redis, push (heartbeat) or group. AI model monitors have their own resource, `okstatus_llm_monitor`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Monitor ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name.",
			},
			"url": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The target: an `http(s)://` URL for `http`, a host for `ping`, `tcp`, `dns` and `grpc` " +
					"(`example.com`, `grpc.example.com:443`), a connection URL for databases " +
					"(`postgres://db.example.com:5432/app`), a host or `smtp://` / `imap://` / `redis://` URL for those, " +
					"a free label for `push` and `group`. Never put a user and password in it: use " +
					"`basic_auth_user` / `basic_auth_password`.",
				Validators: []validator.String{noURLCredentialsValidator{}},
			},
			"type": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Check type: `http`, `tcp`, `ping`, `dns`, `grpc`, `push`, `group`, `postgres`, `mysql`, " +
					"`mongodb`, `smtp`, `imap` or `redis`. Defaults to `http`. Which types your plan includes is " +
					"enforced at `apply`. Changing it replaces the monitor: a type cannot be changed afterwards.",
				Validators: []validator.String{
					stringvalidator.OneOf(monitorTypes...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"interval": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Check interval in seconds (minimum 30). Defaults to 60.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"regions": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Regions to check from — your main region plus secondary regions (max 5 per " +
					"monitor). Any of the 24 public region codes (`fr-dc3`, `fr-sbg`, `ie-dub`, `dk-cph`, " +
					"`de-fra`, `nl-ams`, `es-mad`, `se-sto`, `pl-waw`, `ch-zrh`, `uk-lon`, `it-mil`, `ca-bhs`, " +
					"`us-hil`, `br-sao`, `cl-scl`, `sg-sgp`, `in-mum`, `za-cpt`, " +
					"`za-jnb`, `au-syd`, `au-mel`, `bh-man`, `qa-doh`), `private` (any of your own deployed agents) " +
					"or `private:<agent id>` (one agent — the `region` attribute of `data.okstatus_private_agents`); " +
					"the pre-rename bare country codes (`fr`, `de`, `nl`, …) are still accepted but deprecated. " +
					"Defaults to `[\"fr-dc3\"]`. Which of these your plan actually allows is enforced " +
					"server-side (`PLAN_ALLOWED_REGIONS`) — a region your plan doesn't include fails at `apply` " +
					"with a 400, not at `plan`.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(regionValidator()),
				},
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"accepted_status_codes": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "HTTP status codes (or ranges, e.g. `\"200-299\"`) treated as a successful " +
					"check. Defaults to `[\"200-299\"]`. Only meaningful for `type = \"http\"`.",
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 20),
					listvalidator.ValueStringsAre(stringvalidator.RegexMatches(
						statusCodeRegexp, "must be a 3-digit code or range, e.g. \"200\" or \"200-299\"",
					)),
				},
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			// The attributes below were added in v1.6. Each is Optional and
			// Computed with no default on purpose: left out of the
			// configuration, it keeps whatever the monitor already has —
			// upgrading the provider must not wipe tags, resume a paused
			// monitor or drop a keyword that were set in the dashboard.
			"tags": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Tags (5 at most, 30 characters each). Left out, the monitor's current tags are " +
					"kept; `[]` removes them all. If your organization lowercases tags (Settings → Tags), write " +
					"them in lowercase here, or `apply` reports an inconsistent result.",
				Validators: []validator.Set{
					setvalidator.SizeAtMost(5),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 30)),
				},
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"paused": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`true` stops the checks, `false` resumes them. Left out, the monitor stays as it is.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"muted": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`true` stops the alerts while the checks keep running. Left out, the monitor stays as it is.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"keyword": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`http` only: text the response body must contain. `\"\"` removes the check; " +
					"left out, the current keyword is kept.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(1000)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"follow_redirects": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: follow 3xx redirects (the default for a new monitor) instead of judging the redirect response itself.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"port": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`tcp` only: port to connect to.",
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"dns_record_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`dns` only: record type to query — `A`, `AAAA`, `CNAME`, `MX`, `NS`, `TXT`, `SOA`, `CAA` or `SRV`.",
				Validators:          []validator.String{stringvalidator.OneOf(dnsRecordTypes...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dns_expected_value": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`dns` only: value the answer must contain. `\"\"` removes the expectation; " +
					"left out, the current one is kept.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(500)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"http_method": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: request method — `GET` (what a new monitor uses), `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` or `OPTIONS`.",
				Validators:          []validator.String{stringvalidator.OneOf(httpMethods...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"http_headers": schema.MapAttribute{
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
				MarkdownDescription: "`http` only: request headers, e.g. `{ Authorization = \"Bearer …\" }`. Sensitive: " +
					"OKStatus never returns them, so Terraform cannot detect a change made in the dashboard — it " +
					"sends the headers when this attribute changes. Removing the attribute (or `{}`) removes " +
					"the headers Terraform had set.",
				Validators: []validator.Map{
					mapvalidator.KeysAre(stringvalidator.LengthBetween(1, 256)),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtMost(4096)),
				},
			},
			"http_body": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`http` only: request body (10 000 characters at most). `\"\"` removes it; " +
					"left out, the current body is kept.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(10000)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"http_body_encoding": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: how `http_body` is sent — `json`, `form`, `xml` or `text` (sets the `Content-Type`).",
				Validators:          []validator.String{stringvalidator.OneOf(httpBodyEncodings...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ignore_tls": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: accept an invalid or self-signed certificate.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"retries": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Consecutive failed checks tolerated before the monitor is down (0–5; a new monitor uses 1).",
				Validators:          []validator.Int64{int64validator.Between(0, 5)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"severity_3xx": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: a redirect response that is not accepted counts as `degraded` (what a new monitor uses) or `down`.",
				Validators:          []validator.String{stringvalidator.OneOf("degraded", "down")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"latency_threshold_ms": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Response time, in milliseconds, above which the monitor turns `degraded` and alerts (50–60000). It never makes the monitor down and does not lower its uptime. Omit it to turn the alert off.",
				Validators:          []validator.Int64{int64validator.Between(50, 60000)},
			},
			"latency_public": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`false` keeps slowness off public status pages: the monitor shows as operational there while the dashboard and the alerts still report it. Defaults to `true`.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"latency_anomaly": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Alert when the response time moves well out of its learned usual range, region by region and by hour of the week (Max and Enterprise; 7 days of learning first). Refused at `apply` on a lower plan.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"keyword_invert": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`http` only: `true` makes the monitor down when `keyword` **is** found.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"json_path": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`http` only: path of the value to check in a JSON response (e.g. `$.status`), with " +
					"`json_operator` and `json_expected_value`. Cannot be combined with `keyword`. `\"\"` removes the assertion.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(500)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"json_operator": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`==`, `!=`, `>`, `<`, `>=`, `<=` or `contains`.",
				Validators:          []validator.String{stringvalidator.OneOf(jsonOperators...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"json_expected_value": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Value the JSON path is compared with.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(1000)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dns_server": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "`dns` only: resolver to query instead of the default one. `\"\"` goes back to the default.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(253)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"db_query": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`postgres`, `mysql`, `mongodb`: query run on each check (SQL, or a MongoDB command as JSON). " +
					"`\"\"` removes it — the check then only connects.",
				Validators:    []validator.String{stringvalidator.LengthAtMost(4000)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"grace_period": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "`push` only: seconds without a ping before the monitor is down (defaults to `interval`). " +
					"Set at creation: changing it replaces the monitor, and so its `push_token`.",
				Validators: []validator.Int64{int64validator.Between(30, 86400)},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"child_monitor_ids": schema.SetAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "`group` only: the monitors this one aggregates (1 to 50).",
				Validators:          []validator.Set{setvalidator.SizeBetween(1, 50)},
				PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			},
			"push_token": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				MarkdownDescription: "`push` only: the token of the monitor's ping URL (`/push/<token>` on the address shown on " +
					"the monitor's page). Anyone holding it can report the job as alive.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"basic_auth_user": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "User of the monitored endpoint: HTTP Basic auth, or the account of a database, SMTP, IMAP " +
					"or Redis monitor. Like every credential here, OKStatus never returns it: Terraform sends it when " +
					"it changes, and removes it from the monitor when you remove the attribute.",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 256)},
			},
			"basic_auth_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Password that goes with `basic_auth_user`.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 256)},
			},
			"auth": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "`http` only: authentication other than Basic. Write the attributes of the chosen `type`: " +
					"`bearer` → `token`; `apikey` → `name`, `value`, `in`; `cookie` → `cookie`; `oauth2` (client " +
					"credentials) → `token_url`, `client_id`, `client_secret`, optionally `scope`, `audience`, " +
					"`auth_style`; `mtls` → `cert`, `key`, optionally `ca`. Secrets are never returned by OKStatus: " +
					"Terraform sends the block when it changes and cannot see an edit made in the dashboard. " +
					"Removing the block removes the authentication.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`bearer`, `apikey`, `cookie`, `oauth2` or `mtls`.",
						Validators:          []validator.String{stringvalidator.OneOf("bearer", "apikey", "cookie", "oauth2", "mtls")},
					},
					"token":         schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "`bearer`: the token, without the `Bearer ` prefix."},
					"in":            schema.StringAttribute{Optional: true, MarkdownDescription: "`apikey`: `header` (default) or `query`.", Validators: []validator.String{stringvalidator.OneOf("header", "query")}},
					"name":          schema.StringAttribute{Optional: true, MarkdownDescription: "`apikey`: name of the header or query parameter."},
					"value":         schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "`apikey`: the key."},
					"cookie":        schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "`cookie`: the `Cookie` header value, e.g. `session=…; csrf=…`."},
					"token_url":     schema.StringAttribute{Optional: true, MarkdownDescription: "`oauth2`: token endpoint."},
					"client_id":     schema.StringAttribute{Optional: true, MarkdownDescription: "`oauth2`: client id."},
					"client_secret": schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "`oauth2`: client secret."},
					"scope":         schema.StringAttribute{Optional: true, MarkdownDescription: "`oauth2`: requested scope."},
					"audience":      schema.StringAttribute{Optional: true, MarkdownDescription: "`oauth2`: requested audience."},
					"auth_style":    schema.StringAttribute{Optional: true, MarkdownDescription: "`oauth2`: how the client authenticates to the token endpoint — `basic` (default) or `post`.", Validators: []validator.String{stringvalidator.OneOf("basic", "post")}},
					"cert":          schema.StringAttribute{Optional: true, MarkdownDescription: "`mtls`: client certificate, PEM."},
					"key":           schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "`mtls`: unencrypted private key, PEM (PKCS#8)."},
					"ca":            schema.StringAttribute{Optional: true, MarkdownDescription: "`mtls`: CA certificate to verify the server with, PEM."},
				},
			},
			"alert_channel_ids": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "The complete set of alert channels attached to the monitor (ids of " +
					"`okstatus_alert_channel` resources or of `data.okstatus_alert_channels`); `[]` detaches " +
					"them all. Left out, Terraform does not manage the monitor's channels at all — whatever " +
					"is attached in the dashboard, or inherited from a monitor group, stays. If the monitor " +
					"is in an `okstatus_monitor_group` that has its own `alert_channel_ids`, list those here " +
					"too: a monitor joining a group receives the group's channels.",
				Validators: []validator.Set{setvalidator.SizeAtMost(20)},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current monitor status as last reported by OKStatus (`pending`, `active`, `paused`, `down`).",
			},
		},
	}
}

func (r *MonitorResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *MonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data monitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MonitorCreateInput{
		Name: data.Name.ValueString(),
		URL:  data.URL.ValueString(),
		Type: data.Type.ValueString(),
	}
	if !data.Interval.IsNull() && !data.Interval.IsUnknown() {
		in.Interval = data.Interval.ValueInt64()
	}
	if !data.Regions.IsNull() && !data.Regions.IsUnknown() {
		resp.Diagnostics.Append(data.Regions.ElementsAs(ctx, &in.Regions, false)...)
	}
	if !data.AcceptedStatusCodes.IsNull() && !data.AcceptedStatusCodes.IsUnknown() {
		resp.Diagnostics.Append(data.AcceptedStatusCodes.ElementsAs(ctx, &in.AcceptedStatusCodes, false)...)
	}
	if known(data.Tags) {
		resp.Diagnostics.Append(data.Tags.ElementsAs(ctx, &in.Tags, false)...)
	}
	in.Keyword = data.Keyword.ValueString()
	in.FollowRedirects = boolPtrOrNil(data.FollowRedirects)
	in.Port = int64PtrOrNil(data.Port)
	in.DNSRecordType = data.DNSRecordType.ValueString()
	in.DNSExpectedValue = data.DNSExpectedValue.ValueString()
	in.HTTPMethod = data.HTTPMethod.ValueString()
	in.HTTPBody = data.HTTPBody.ValueString()
	in.HTTPBodyEncoding = data.HTTPBodyEncoding.ValueString()
	in.IgnoreTLS = boolPtrOrNil(data.IgnoreTLS)
	if known(data.HTTPHeaders) {
		resp.Diagnostics.Append(data.HTTPHeaders.ElementsAs(ctx, &in.HTTPHeaders, false)...)
	}
	in.Retries = int64PtrOrNil(data.Retries)
	in.Severity3xx = data.Severity3xx.ValueString()
	in.LatencyThresholdMs = int64PtrOrNil(data.LatencyThresholdMs)
	in.LatencyPublic = boolPtrOrNil(data.LatencyPublic)
	in.LatencyAnomaly = boolPtrOrNil(data.LatencyAnomaly)
	in.KeywordInvert = boolPtrOrNil(data.KeywordInvert)
	in.JSONPath = data.JSONPath.ValueString()
	in.JSONOperator = data.JSONOperator.ValueString()
	in.JSONExpectedValue = data.JSONExpectedValue.ValueString()
	in.DNSServer = data.DNSServer.ValueString()
	in.DBQuery = data.DBQuery.ValueString()
	in.GracePeriod = int64PtrOrNil(data.GracePeriod)
	if known(data.ChildMonitorIDs) {
		resp.Diagnostics.Append(data.ChildMonitorIDs.ElementsAs(ctx, &in.ChildMonitorIDs, false)...)
	}
	in.BasicAuthUser = data.BasicAuthUser.ValueString()
	in.BasicAuthPass = data.BasicAuthPassword.ValueString()
	if known(data.Auth) {
		auth, d := authConfigFrom(ctx, data.Auth)
		resp.Diagnostics.Append(d...)
		in.AuthConfig = auth
	}
	if resp.Diagnostics.HasError() {
		return
	}
	wantPaused := data.Paused.ValueBool()
	wantMuted := data.Muted.ValueBool()
	channels := data.AlertChannelIDs

	m, err := r.client.CreateMonitor(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create monitor", err.Error())
		return
	}
	// The monitor exists from here on: it is saved before the follow-up
	// calls, so a failure below leaves a tainted resource Terraform can
	// replace, not an orphan it has never heard of.
	resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)
	data.AlertChannelIDs = types.SetNull(types.StringType)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A monitor is always created active and unmuted: pausing or muting it
	// is a second write (same as in the dashboard).
	if wantPaused || wantMuted {
		patch := client.MonitorUpdateInput{}
		if wantPaused {
			status := "paused"
			patch.Status = &status
		}
		if wantMuted {
			patch.IsMuted = &wantMuted
		}
		m, err = r.client.UpdateMonitor(ctx, m.ID, patch)
		if err != nil {
			resp.Diagnostics.AddError("Monitor created, but it could not be paused or muted", err.Error())
			return
		}
		resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)
	}

	if !channels.IsNull() && !channels.IsUnknown() {
		resp.Diagnostics.Append(r.setAlertChannels(ctx, m.ID, channels, &data)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data monitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.GetMonitor(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)

	// Only when Terraform manages the channels (attribute set): otherwise
	// they are none of this resource's business, and not worth a request.
	if !data.AlertChannelIDs.IsNull() {
		ids, err := r.client.GetMonitorAlertChannels(ctx, m.ID)
		if err != nil {
			resp.Diagnostics.AddError("Unable to read the monitor's alert channels", err.Error())
			return
		}
		set, d := types.SetValueFrom(ctx, types.StringType, ids)
		resp.Diagnostics.Append(d...)
		data.AlertChannelIDs = set
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior monitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, d := monitorUpdateInput(ctx, data, prior)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	channels := data.AlertChannelIDs

	m, err := r.client.UpdateMonitor(ctx, data.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)
	// Removed from the configuration = no longer managed: the channels stay
	// attached and the attribute simply leaves the state.
	if known(channels) && !channels.Equal(prior.AlertChannelIDs) {
		resp.Diagnostics.Append(r.setAlertChannels(ctx, m.ID, channels, &data)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// monitorUpdateInput builds the PATCH body from the plan. `name` and `url`
// are always sent; everything else only when the plan holds a decided value,
// and the settings added in v1.6 only when that value differs from the state.
func monitorUpdateInput(ctx context.Context, data, prior monitorResourceModel) (client.MonitorUpdateInput, diag.Diagnostics) {
	var diags diag.Diagnostics
	name := data.Name.ValueString()
	url := data.URL.ValueString()
	in := client.MonitorUpdateInput{
		Name: &name,
		URL:  &url,
		// Left out of the configuration, the interval used to be sent as 0
		// (the zero value of an unknown plan value), which the API refuses:
		// any update of a monitor without an explicit `interval` failed.
		Interval: int64PtrOrNil(data.Interval),
	}
	if !data.Regions.IsNull() && !data.Regions.IsUnknown() {
		diags.Append(data.Regions.ElementsAs(ctx, &in.Regions, false)...)
	}
	if !data.AcceptedStatusCodes.IsNull() && !data.AcceptedStatusCodes.IsUnknown() {
		diags.Append(data.AcceptedStatusCodes.ElementsAs(ctx, &in.AcceptedStatusCodes, false)...)
	}
	if known(data.Tags) && !data.Tags.Equal(prior.Tags) {
		tags := []string{}
		diags.Append(data.Tags.ElementsAs(ctx, &tags, false)...)
		in.Tags = &tags
	}
	// Only when it changes: `status` also holds what the checks found
	// ("down", "pending"), and writing "active" over it on an unrelated
	// update (a rename) would erase that.
	if known(data.Paused) && !data.Paused.Equal(prior.Paused) {
		status := "active"
		if data.Paused.ValueBool() {
			status = "paused"
		}
		in.Status = &status
	}
	if !data.Muted.Equal(prior.Muted) {
		in.IsMuted = boolPtrOrNil(data.Muted)
	}
	if !data.FollowRedirects.Equal(prior.FollowRedirects) {
		in.FollowRedirects = boolPtrOrNil(data.FollowRedirects)
	}
	if !data.Port.Equal(prior.Port) {
		in.Port = int64PtrOrNil(data.Port)
	}
	// "" means "none" for these two: the API removes the value on null.
	if known(data.Keyword) && !data.Keyword.Equal(prior.Keyword) {
		if data.Keyword.ValueString() == "" {
			in.Null = append(in.Null, "keyword")
		} else {
			in.Keyword = stringPtrVal(data.Keyword)
		}
	}
	if known(data.DNSExpectedValue) && !data.DNSExpectedValue.Equal(prior.DNSExpectedValue) {
		if data.DNSExpectedValue.ValueString() == "" {
			in.Null = append(in.Null, "dnsExpectedValue")
		} else {
			in.DNSExpectedValue = stringPtrVal(data.DNSExpectedValue)
		}
	}
	if known(data.DNSRecordType) && data.DNSRecordType.ValueString() != "" && !data.DNSRecordType.Equal(prior.DNSRecordType) {
		in.DNSRecordType = stringPtrVal(data.DNSRecordType)
	}
	if known(data.HTTPMethod) && !data.HTTPMethod.Equal(prior.HTTPMethod) {
		in.HTTPMethod = stringPtrVal(data.HTTPMethod)
	}
	if known(data.HTTPBody) && !data.HTTPBody.Equal(prior.HTTPBody) {
		if data.HTTPBody.ValueString() == "" {
			in.Null = append(in.Null, "httpBody")
		} else {
			in.HTTPBody = stringPtrVal(data.HTTPBody)
		}
	}
	if known(data.HTTPBodyEncoding) && data.HTTPBodyEncoding.ValueString() != "" && !data.HTTPBodyEncoding.Equal(prior.HTTPBodyEncoding) {
		in.HTTPBodyEncoding = stringPtrVal(data.HTTPBodyEncoding)
	}
	if !data.IgnoreTLS.Equal(prior.IgnoreTLS) {
		in.IgnoreTLS = boolPtrOrNil(data.IgnoreTLS)
	}
	// The API never returns the headers: the state holds what Terraform
	// last sent, and they are sent again only when the configuration changes.
	// Removing the attribute removes the headers Terraform had set.
	if !data.HTTPHeaders.IsUnknown() && !data.HTTPHeaders.Equal(prior.HTTPHeaders) {
		headers := map[string]string{}
		if !data.HTTPHeaders.IsNull() {
			diags.Append(data.HTTPHeaders.ElementsAs(ctx, &headers, false)...)
		}
		in.HTTPHeaders = &headers
	}

	if !data.Retries.Equal(prior.Retries) {
		in.Retries = int64PtrOrNil(data.Retries)
	}
	if known(data.Severity3xx) && !data.Severity3xx.Equal(prior.Severity3xx) {
		in.Severity3xx = stringPtrVal(data.Severity3xx)
	}
	if !data.LatencyThresholdMs.Equal(prior.LatencyThresholdMs) {
		if data.LatencyThresholdMs.IsNull() {
			in.Null = append(in.Null, "latencyThresholdMs")
		} else {
			in.LatencyThresholdMs = int64PtrOrNil(data.LatencyThresholdMs)
		}
	}
	if known(data.LatencyPublic) && !data.LatencyPublic.Equal(prior.LatencyPublic) {
		in.LatencyPublic = boolPtrOrNil(data.LatencyPublic)
	}
	if known(data.LatencyAnomaly) && !data.LatencyAnomaly.Equal(prior.LatencyAnomaly) {
		in.LatencyAnomaly = boolPtrOrNil(data.LatencyAnomaly)
	}
	if !data.KeywordInvert.Equal(prior.KeywordInvert) {
		in.KeywordInvert = boolPtrOrNil(data.KeywordInvert)
	}
	// "" removes the value (explicit null), as for keyword.
	for _, f := range []struct {
		plan, prior types.String
		name        string
		set         **string
	}{
		{data.JSONPath, prior.JSONPath, "jsonPath", &in.JSONPath},
		{data.JSONExpectedValue, prior.JSONExpectedValue, "jsonExpectedValue", &in.JSONExpectedValue},
		{data.DNSServer, prior.DNSServer, "dnsServer", &in.DNSServer},
		{data.DBQuery, prior.DBQuery, "dbQuery", &in.DBQuery},
	} {
		if !known(f.plan) || f.plan.Equal(f.prior) {
			continue
		}
		if f.plan.ValueString() == "" {
			in.Null = append(in.Null, f.name)
		} else {
			*f.set = stringPtrVal(f.plan)
		}
	}
	if known(data.JSONOperator) && data.JSONOperator.ValueString() != "" && !data.JSONOperator.Equal(prior.JSONOperator) {
		in.JSONOperator = stringPtrVal(data.JSONOperator)
	}
	if known(data.ChildMonitorIDs) && !data.ChildMonitorIDs.Equal(prior.ChildMonitorIDs) && len(data.ChildMonitorIDs.Elements()) > 0 {
		diags.Append(data.ChildMonitorIDs.ElementsAs(ctx, &in.ChildMonitorIDs, false)...)
	}

	// Credentials are never read back: the state is what Terraform last
	// sent. A changed value is sent; a removed attribute removes the
	// credential from the monitor (explicit null).
	for _, f := range []struct {
		plan, prior types.String
		name        string
		set         **string
	}{
		{data.BasicAuthUser, prior.BasicAuthUser, "basicAuthUser", &in.BasicAuthUser},
		{data.BasicAuthPassword, prior.BasicAuthPassword, "basicAuthPass", &in.BasicAuthPass},
	} {
		if f.plan.IsUnknown() || f.plan.Equal(f.prior) {
			continue
		}
		if f.plan.IsNull() {
			in.Null = append(in.Null, f.name)
		} else {
			*f.set = stringPtrVal(f.plan)
		}
	}
	if !data.Auth.IsUnknown() && !data.Auth.Equal(prior.Auth) {
		if data.Auth.IsNull() {
			in.Null = append(in.Null, "authConfig")
		} else {
			auth, d := authConfigFrom(ctx, data.Auth)
			diags.Append(d...)
			in.AuthConfig = auth
		}
	}
	return in, diags
}

// setAlertChannels replaces the monitor's attached channels with the planned
// set and stores what the API kept.
func (r *MonitorResource) setAlertChannels(ctx context.Context, monitorID string, planned types.Set, data *monitorResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	ids := []string{}
	diags.Append(planned.ElementsAs(ctx, &ids, false)...)
	if diags.HasError() {
		return diags
	}
	kept, err := r.client.SetMonitorAlertChannels(ctx, monitorID, ids)
	if err != nil {
		diags.AddError("Unable to set the monitor's alert channels", err.Error())
		return diags
	}
	set, d := types.SetValueFrom(ctx, types.StringType, kept)
	diags.Append(d...)
	data.AlertChannelIDs = set
	return diags
}

func (r *MonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data monitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMonitor(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete monitor", err.Error())
	}
}

func (r *MonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func writeMonitor(ctx context.Context, data *monitorResourceModel, m *client.Monitor) diag.Diagnostics {
	var diags diag.Diagnostics

	data.ID = types.StringValue(m.ID)
	data.Name = types.StringValue(m.Name)
	data.URL = types.StringValue(m.URL)
	data.Type = types.StringValue(m.Type)
	data.Interval = types.Int64Value(m.Interval)
	data.Status = types.StringValue(m.Status)

	regions, d := types.ListValueFrom(ctx, types.StringType, m.Regions)
	diags.Append(d...)
	data.Regions = regions

	codes, d := types.ListValueFrom(ctx, types.StringType, m.AcceptedStatusCodes)
	diags.Append(d...)
	data.AcceptedStatusCodes = codes

	tagValues := m.Tags
	if tagValues == nil {
		tagValues = []string{}
	}
	tags, d := types.SetValueFrom(ctx, types.StringType, tagValues)
	diags.Append(d...)
	data.Tags = tags

	data.Paused = types.BoolValue(m.Status == "paused")
	data.Muted = types.BoolValue(m.IsMuted != nil && *m.IsMuted)
	// Redirects are followed unless the monitor says otherwise (column
	// default in ok-status-backend).
	data.FollowRedirects = types.BoolValue(m.FollowRedirects == nil || *m.FollowRedirects)
	// "" stands for "none" on the optional strings: a computed attribute
	// has to hold a value the configuration can also express.
	data.Keyword = stringOrEmpty(m.Keyword)
	data.DNSRecordType = stringOrEmpty(m.DNSRecordType)
	data.DNSExpectedValue = stringOrEmpty(m.DNSExpectedValue)
	// A monitor without a stored method sends GET. http_headers is not
	// touched here: it stays what the configuration says (see the schema).
	if m.HTTPMethod == nil || *m.HTTPMethod == "" {
		data.HTTPMethod = types.StringValue("GET")
	} else {
		data.HTTPMethod = types.StringValue(*m.HTTPMethod)
	}
	data.HTTPBody = stringOrEmpty(m.HTTPBody)
	data.HTTPBodyEncoding = stringOrEmpty(m.HTTPBodyEncoding)
	data.IgnoreTLS = types.BoolValue(m.IgnoreTLS != nil && *m.IgnoreTLS)
	if m.Retries == nil {
		data.Retries = types.Int64Value(1)
	} else {
		data.Retries = types.Int64Value(*m.Retries)
	}
	if m.Severity3xx == nil || *m.Severity3xx == "" {
		data.Severity3xx = types.StringValue("degraded")
	} else {
		data.Severity3xx = types.StringValue(*m.Severity3xx)
	}
	if m.LatencyThresholdMs == nil {
		data.LatencyThresholdMs = types.Int64Null()
	} else {
		data.LatencyThresholdMs = types.Int64Value(*m.LatencyThresholdMs)
	}
	data.LatencyPublic = types.BoolValue(m.LatencyPublic == nil || *m.LatencyPublic)
	data.LatencyAnomaly = types.BoolValue(m.LatencyAnomaly != nil && *m.LatencyAnomaly)
	data.KeywordInvert = types.BoolValue(m.KeywordInvert != nil && *m.KeywordInvert)
	data.JSONPath = stringOrEmpty(m.JSONPath)
	data.JSONOperator = stringOrEmpty(m.JSONOperator)
	data.JSONExpectedValue = stringOrEmpty(m.JSONExpectedValue)
	data.DNSServer = stringOrEmpty(m.DNSServer)
	data.DBQuery = stringOrEmpty(m.DBQuery)
	if m.GracePeriod == nil {
		data.GracePeriod = types.Int64Null()
	} else {
		data.GracePeriod = types.Int64Value(*m.GracePeriod)
	}
	children, d := types.SetValueFrom(ctx, types.StringType, append([]string{}, m.ChildMonitorIDs...))
	diags.Append(d...)
	data.ChildMonitorIDs = children
	// "" for a monitor that is not a push monitor: a known value keeps the
	// attribute out of every later plan.
	data.PushToken = stringOrEmpty(m.PushToken)
	// basic_auth_*, auth and http_headers are not touched: OKStatus returns
	// them masked, the state keeps what the configuration says.
	if m.Port == nil {
		data.Port = types.Int64Null()
	} else {
		data.Port = types.Int64Value(*m.Port)
	}

	return diags
}
