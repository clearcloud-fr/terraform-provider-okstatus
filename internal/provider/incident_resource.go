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

var _ resource.Resource = &IncidentResource{}
var _ resource.ResourceWithImportState = &IncidentResource{}

func NewIncidentResource() resource.Resource {
	return &IncidentResource{}
}

type IncidentResource struct {
	client *client.Client
}

type incidentResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Title     types.String `tfsdk:"title"`
	Severity  types.String `tfsdk:"severity"`
	MonitorID types.String `tfsdk:"monitor_id"`
	Status    types.String `tfsdk:"status"`
	Message   types.String `tfsdk:"message"`
}

func (r *IncidentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_incident"
}

func (r *IncidentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OKStatus incident tied to a monitor. Unlike most resources, `status`/`message` don't get " +
			"PATCHed as plain fields — api-v1 appends a timeline update each time `status` changes, so `message` is required " +
			"whenever `status` is changed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"severity": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.OneOf("minor", "major", "critical")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"monitor_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "One of `investigating`, `identified`, `monitoring`, `resolved`.",
				Validators: []validator.String{
					stringvalidator.OneOf("investigating", "identified", "monitoring", "resolved"),
				},
			},
			"message": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Timeline message posted alongside the current `status`.",
			},
		},
	}
}

func (r *IncidentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *IncidentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data incidentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateIncident(ctx, client.IncidentCreateInput{
		Title:     data.Title.ValueString(),
		Severity:  data.Severity.ValueString(),
		MonitorID: data.MonitorID.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create incident", err.Error())
		return
	}

	// Creation always starts an incident at "investigating" — if the plan
	// asked for a different initial status, push that status transition
	// immediately so state matches what was declared.
	status := data.Status.ValueString()
	if status != "" && status != created.Status {
		updated, err := r.client.UpdateIncidentStatus(ctx, created.ID, client.IncidentStatusUpdateInput{
			Status:  status,
			Message: data.Message.ValueString(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Unable to set initial incident status", err.Error())
			return
		}
		created = updated
	}

	writeIncident(&data, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IncidentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data incidentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inc, err := r.client.GetIncident(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read incident", err.Error())
		return
	}

	writeIncident(&data, inc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *IncidentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data incidentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateIncidentStatus(ctx, data.ID.ValueString(), client.IncidentStatusUpdateInput{
		Status:  data.Status.ValueString(),
		Message: data.Message.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update incident status", err.Error())
		return
	}

	writeIncident(&data, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete — api-v1 has no DELETE /incidents/:id (incidents are a permanent
// record, resolved rather than removed). Terraform "destroy" here just
// drops it from state; nothing is called against the API. If it isn't
// already resolved, nudge it there first so destroying the resource leaves
// a clean incident behind rather than an abandoned open one.
func (r *IncidentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data incidentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if data.Status.ValueString() != "resolved" {
		_, err := r.client.UpdateIncidentStatus(ctx, data.ID.ValueString(), client.IncidentStatusUpdateInput{
			Status:  "resolved",
			Message: "Resolved by terraform destroy.",
		})
		if err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Unable to resolve incident before removing from state", err.Error())
		}
	}
}

func (r *IncidentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func writeIncident(data *incidentResourceModel, inc *client.Incident) {
	data.ID = types.StringValue(inc.ID)
	data.Title = types.StringValue(inc.Title)
	data.Severity = types.StringValue(inc.Severity)
	data.MonitorID = types.StringValue(inc.MonitorID)
	data.Status = types.StringValue(inc.Status)
	// inc.Message intentionally not overwritten from the API — GET
	// /incidents/:id returns the whole timeline, not "the current message";
	// keep whatever the config/state last set.
}
