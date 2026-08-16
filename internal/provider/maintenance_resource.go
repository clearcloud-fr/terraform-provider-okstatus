package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ resource.Resource = &MaintenanceResource{}
var _ resource.ResourceWithImportState = &MaintenanceResource{}

func NewMaintenanceResource() resource.Resource {
	return &MaintenanceResource{}
}

type MaintenanceResource struct {
	client *client.Client
}

type maintenanceResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Title          types.String `tfsdk:"title"`
	Message        types.String `tfsdk:"message"`
	Status         types.String `tfsdk:"status"`
	ScheduledStart types.String `tfsdk:"scheduled_start"`
	ScheduledEnd   types.String `tfsdk:"scheduled_end"`
	MonitorIDs     types.List   `tfsdk:"monitor_ids"`
}

func (r *MaintenanceResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_maintenance"
}

func (r *MaintenanceResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A scheduled maintenance window for one or more monitors.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title":   schema.StringAttribute{Required: true},
			"message": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "One of `scheduled`, `in_progress`, `completed`, `cancelled` — set by OKStatus based on the schedule, not user-writable at create time.",
			},
			"scheduled_start": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ISO-8601 datetime.",
			},
			"scheduled_end": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ISO-8601 datetime.",
			},
			"monitor_ids": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Validators:  []validator.List{listvalidator.SizeAtLeast(1)},
			},
		},
	}
}

func (r *MaintenanceResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *MaintenanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data maintenanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var monitorIDs []string
	resp.Diagnostics.Append(data.MonitorIDs.ElementsAs(ctx, &monitorIDs, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.CreateMaintenance(ctx, client.MaintenanceCreateInput{
		Title:          data.Title.ValueString(),
		Message:        stringPtrOrNil(data.Message),
		ScheduledStart: data.ScheduledStart.ValueString(),
		ScheduledEnd:   data.ScheduledEnd.ValueString(),
		MonitorIDs:     monitorIDs,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create maintenance", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMaintenance(ctx, &data, m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MaintenanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data maintenanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.GetMaintenance(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read maintenance", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMaintenance(ctx, &data, m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MaintenanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data maintenanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var monitorIDs []string
	resp.Diagnostics.Append(data.MonitorIDs.ElementsAs(ctx, &monitorIDs, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	title := data.Title.ValueString()
	start := data.ScheduledStart.ValueString()
	end := data.ScheduledEnd.ValueString()
	m, err := r.client.UpdateMaintenance(ctx, data.ID.ValueString(), client.MaintenanceUpdateInput{
		Title:          &title,
		Message:        stringPtrOrNil(data.Message),
		ScheduledStart: &start,
		ScheduledEnd:   &end,
		MonitorIDs:     monitorIDs,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update maintenance", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMaintenance(ctx, &data, m)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MaintenanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data maintenanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMaintenance(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete maintenance", err.Error())
	}
}

func (r *MaintenanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func writeMaintenance(ctx context.Context, data *maintenanceResourceModel, m *client.Maintenance) diag.Diagnostics {
	data.ID = types.StringValue(m.ID)
	data.Title = types.StringValue(m.Title)
	data.Message = nullableString(m.Message)
	data.Status = types.StringValue(m.Status)
	data.ScheduledStart = types.StringValue(m.ScheduledStart)
	data.ScheduledEnd = types.StringValue(m.ScheduledEnd)
	l, d := types.ListValueFrom(ctx, types.StringType, m.MonitorIDs)
	data.MonitorIDs = l
	return d
}
