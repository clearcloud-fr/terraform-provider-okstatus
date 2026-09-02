package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var statusCodeRegexp = regexp.MustCompile(statusCodePattern)

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
	"fr-dc3", "fr-rbx", "fr-sbg", "fr-gra",
	"de-fra", "nl-ams", "es-mad", "se-sto", "pl-waw", "ch-zrh", "uk-lon", "it-mil",
	"ca-bhs", "us-hil", "br-sao", "cl-scl", "sg-sgp", "in-mum", "bh-man", "qa-doh",
	"za-cpt", "za-jnb", "au-syd", "au-mel",
	// deprecated dual-accept shims (renamed to cc-city on 2026-08-29)
	"fr", "de", "nl", "es", "se", "pl", "ch", "uk", "it",
	"ca", "us", "br", "cl", "sg", "in", "bh", "qa",
	"private",
}

const statusCodePattern = `^\d{3}(-\d{3})?$`

func (r *MonitorResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *MonitorResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OKStatus monitor (HTTP, TCP, ping or DNS check).",
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
				Required:            true,
				MarkdownDescription: "Target URL to check.",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Check type: `http`, `tcp`, `ping`, or `dns`. Defaults to `http`.",
				Validators: []validator.String{
					stringvalidator.OneOf("http", "tcp", "ping", "dns"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"interval": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Check interval in seconds (minimum 30). Defaults to 60.",
			},
			"regions": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Regions to check from — your main region plus secondary regions (max 5 per " +
					"monitor). Any of the 24 public region codes (`fr-dc3`, `fr-rbx`, `fr-sbg`, `fr-gra`, " +
					"`de-fra`, `nl-ams`, `es-mad`, `se-sto`, `pl-waw`, `ch-zrh`, `uk-lon`, `it-mil`, `ca-bhs`, " +
					"`us-hil`, `br-sao`, `cl-scl`, `sg-sgp`, `in-mum`, `za-cpt`, " +
					"`za-jnb`, `au-syd`, `au-mel`, `bh-man`, `qa-doh`) or `private` (your own deployed agents); " +
					"the pre-rename bare country codes (`fr`, `de`, `nl`, …) are still accepted but deprecated. " +
					"Defaults to `[\"fr-dc3\"]`. Which of these your plan actually allows is enforced " +
					"server-side (`PLAN_ALLOWED_REGIONS`) — a region your plan doesn't include fails at `apply` " +
					"with a 400, not at `plan`.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(validRegionCodes...)),
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
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.CreateMonitor(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)
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
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data monitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	url := data.URL.ValueString()
	interval := data.Interval.ValueInt64()
	in := client.MonitorUpdateInput{
		Name:     &name,
		URL:      &url,
		Interval: &interval,
	}
	if !data.Regions.IsNull() && !data.Regions.IsUnknown() {
		resp.Diagnostics.Append(data.Regions.ElementsAs(ctx, &in.Regions, false)...)
	}
	if !data.AcceptedStatusCodes.IsNull() && !data.AcceptedStatusCodes.IsUnknown() {
		resp.Diagnostics.Append(data.AcceptedStatusCodes.ElementsAs(ctx, &in.AcceptedStatusCodes, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.UpdateMonitor(ctx, data.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitor(ctx, &data, m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
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

	return diags
}
