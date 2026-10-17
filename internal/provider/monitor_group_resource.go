package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ resource.Resource = &MonitorGroupResource{}
var _ resource.ResourceWithImportState = &MonitorGroupResource{}

func NewMonitorGroupResource() resource.Resource {
	return &MonitorGroupResource{}
}

type MonitorGroupResource struct {
	client *client.Client
}

type monitorGroupResourceModel struct {
	ID              types.String  `tfsdk:"id"`
	Name            types.String  `tfsdk:"name"`
	Description     types.String  `tfsdk:"description"`
	Color           types.String  `tfsdk:"color"`
	Tags            types.Set     `tfsdk:"tags"`
	AlertChannelIDs types.Set     `tfsdk:"alert_channel_ids"`
	MonitorIDs      types.Set     `tfsdk:"monitor_ids"`
	SLOTarget       types.Float64 `tfsdk:"slo_target"`
}

func (r *MonitorGroupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_group"
}

func emptyStringSet() types.Set {
	return types.SetValueMust(types.StringType, []attr.Value{})
}

func (r *MonitorGroupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A monitor group — a folder of the Monitors page. A monitor belongs to one group at " +
			"most: listing it in `monitor_ids` takes it out of its previous group. Destroying the group keeps " +
			"its monitors, ungrouped.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Group ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Free text shown under the group's name.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 2000)},
			},
			"color": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Color of the folder: a preset key of the dashboard's palette, as shown by " +
					"`data.okstatus_monitor_groups` on a group colored in the dashboard (e.g. `aurora`, `green`).",
				Validators: []validator.String{stringvalidator.LengthBetween(1, 32)},
			},
			"tags": schema.SetAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "The group's own tags (10 at most). They are not copied onto its monitors.",
				Validators: []validator.Set{
					setvalidator.SizeAtMost(10),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 30)),
				},
			},
			"alert_channel_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "Alert channels of the group: a monitor that joins the group gets them " +
					"attached. Monitors already in the group are not touched when this list changes. If you " +
					"also set `alert_channel_ids` on an `okstatus_monitor` of this group, repeat these ids " +
					"there, or the next `apply` detaches them from that monitor.",
				Validators: []validator.Set{setvalidator.SizeAtMost(50)},
			},
			"monitor_ids": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "The group's monitors (ids of `okstatus_monitor` or `okstatus_llm_monitor` " +
					"resources). This is the complete membership: a monitor moved into the group from the " +
					"dashboard is taken out again at the next `apply`.",
				Validators: []validator.Set{setvalidator.SizeAtMost(500)},
			},
			"slo_target": schema.Float64Attribute{
				Optional: true,
				MarkdownDescription: "Availability objective of the group in percent over 30 rolling days (e.g. `99.5`). " +
					"The group counts as down whenever one of its monitors is; budget notices go to the " +
					"group's `alert_channel_ids`. Max and Enterprise; refused at `apply` on a lower plan. " +
					"Omit it for no SLO.",
				Validators: []validator.Float64{float64validator.Between(50, 99.999)},
			},
		},
	}
}

func (r *MonitorGroupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func setToStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if known(set) {
		diags.Append(set.ElementsAs(ctx, &out, false)...)
	}
	return out
}

func (r *MonitorGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data monitorGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MonitorGroupCreateInput{
		Name:            data.Name.ValueString(),
		Description:     data.Description.ValueString(),
		Color:           stringPtrOrNil(data.Color),
		Tags:            setToStrings(ctx, data.Tags, &resp.Diagnostics),
		AlertChannelIDs: setToStrings(ctx, data.AlertChannelIDs, &resp.Diagnostics),
		MonitorIDs:      setToStrings(ctx, data.MonitorIDs, &resp.Diagnostics),
		SLOTarget:       float64PtrOrNil(data.SLOTarget),
	}
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.client.CreateMonitorGroup(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create monitor group", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitorGroup(ctx, &data, g)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data monitorGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.client.GetMonitorGroup(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read monitor group", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitorGroup(ctx, &data, g)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior monitorGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.MonitorGroupUpdateInput{
		Name:            data.Name.ValueString(),
		Description:     data.Description.ValueString(),
		Color:           stringPtrOrNil(data.Color),
		Tags:            setToStrings(ctx, data.Tags, &resp.Diagnostics),
		AlertChannelIDs: setToStrings(ctx, data.AlertChannelIDs, &resp.Diagnostics),
		SLOTarget:       float64PtrOrNil(data.SLOTarget),
	}
	// Membership is its own write server-side, and joining a group attaches
	// the group's channels: only send it when it actually changes.
	if !data.MonitorIDs.Equal(prior.MonitorIDs) {
		ids := setToStrings(ctx, data.MonitorIDs, &resp.Diagnostics)
		in.MonitorIDs = &ids
	}
	if resp.Diagnostics.HasError() {
		return
	}

	g, err := r.client.UpdateMonitorGroup(ctx, data.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update monitor group", err.Error())
		return
	}

	resp.Diagnostics.Append(writeMonitorGroup(ctx, &data, g)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MonitorGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data monitorGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMonitorGroup(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete monitor group", err.Error())
	}
}

func (r *MonitorGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func stringSet(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if values == nil {
		values = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return set
}

func writeMonitorGroup(ctx context.Context, data *monitorGroupResourceModel, g *client.MonitorGroup) diag.Diagnostics {
	var diags diag.Diagnostics

	data.ID = types.StringValue(g.ID)
	data.Name = types.StringValue(g.Name)
	// The API stores "no description" as null or "": both are "not set".
	if g.Description == nil || *g.Description == "" {
		data.Description = types.StringNull()
	} else {
		data.Description = types.StringValue(*g.Description)
	}
	data.Color = nullableString(g.Color)
	data.Tags = stringSet(ctx, g.Tags, &diags)
	data.AlertChannelIDs = stringSet(ctx, g.AlertChannelIDs, &diags)
	data.MonitorIDs = stringSet(ctx, g.MonitorIDs, &diags)
	if g.SLOTarget == nil {
		data.SLOTarget = types.Float64Null()
	} else {
		data.SLOTarget = types.Float64Value(*g.SLOTarget)
	}

	return diags
}
