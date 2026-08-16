package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ resource.Resource = &MonitorResource{}
var _ resource.ResourceWithImportState = &MonitorResource{}

func NewMonitorResource() resource.Resource {
	return &MonitorResource{}
}

type MonitorResource struct {
	client *client.Client
}

type monitorResourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	URL      types.String `tfsdk:"url"`
	Type     types.String `tfsdk:"type"`
	Interval types.Int64  `tfsdk:"interval"`
	Status   types.String `tfsdk:"status"`
}

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

	m, err := r.client.CreateMonitor(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create monitor", err.Error())
		return
	}

	writeMonitor(&data, m)
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

	writeMonitor(&data, m)
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
	m, err := r.client.UpdateMonitor(ctx, data.ID.ValueString(), client.MonitorUpdateInput{
		Name:     &name,
		URL:      &url,
		Interval: &interval,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update monitor", err.Error())
		return
	}

	writeMonitor(&data, m)
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

func writeMonitor(data *monitorResourceModel, m *client.Monitor) {
	data.ID = types.StringValue(m.ID)
	data.Name = types.StringValue(m.Name)
	data.URL = types.StringValue(m.URL)
	data.Type = types.StringValue(m.Type)
	data.Interval = types.Int64Value(m.Interval)
	data.Status = types.StringValue(m.Status)
}
