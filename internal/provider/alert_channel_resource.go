package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ resource.Resource = &AlertChannelResource{}
var _ resource.ResourceWithImportState = &AlertChannelResource{}

func NewAlertChannelResource() resource.Resource {
	return &AlertChannelResource{}
}

type AlertChannelResource struct {
	client *client.Client
}

type alertChannelResourceModel struct {
	ID      types.String `tfsdk:"id"`
	Type    types.String `tfsdk:"type"`
	Config  types.Map    `tfsdk:"config"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

func (r *AlertChannelResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channel"
}

func (r *AlertChannelResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OKStatus alert channel. The `config` shape depends on `type` — e.g. `{ email = \"..." +
			"\" }` for `email`, `{ webhook_url = \"...\" }` for `discord`/`slack`; ok-status-backend validates the exact " +
			"shape per type, not this provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"email", "discord", "slack", "webhook", "telegram",
						"msteams", "splunk", "ntfy", "googlechat", "pagerduty",
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"config": schema.MapAttribute{
				Required:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Channel configuration, string key/value pairs (e.g. `webhook_url`, `email`). Changing it replaces the resource — api-v1 has no PATCH for config, only for `enabled`.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				Computed: true,
			},
		},
	}
}

func (r *AlertChannelResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *AlertChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data alertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config := map[string]string{}
	resp.Diagnostics.Append(data.Config.ElementsAs(ctx, &config, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.AlertChannelCreateInput{Type: data.Type.ValueString(), Config: config}
	if !data.Enabled.IsNull() && !data.Enabled.IsUnknown() {
		v := data.Enabled.ValueBool()
		in.Enabled = &v
	}

	ch, err := r.client.CreateAlertChannel(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create alert channel", err.Error())
		return
	}

	resp.Diagnostics.Append(writeAlertChannel(ctx, &data, ch)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AlertChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data alertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ch, err := r.client.GetAlertChannel(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read alert channel", err.Error())
		return
	}

	resp.Diagnostics.Append(writeAlertChannel(ctx, &data, ch)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AlertChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data alertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ch, err := r.client.UpdateAlertChannel(ctx, data.ID.ValueString(), client.AlertChannelUpdateInput{
		Enabled: data.Enabled.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update alert channel", err.Error())
		return
	}

	resp.Diagnostics.Append(writeAlertChannel(ctx, &data, ch)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *AlertChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data alertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAlertChannel(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete alert channel", err.Error())
	}
}

func (r *AlertChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func writeAlertChannel(ctx context.Context, data *alertChannelResourceModel, ch *client.AlertChannel) diag.Diagnostics {
	data.ID = types.StringValue(ch.ID)
	data.Type = types.StringValue(ch.Type)
	data.Enabled = types.BoolValue(ch.Enabled)
	m, diags := types.MapValueFrom(ctx, types.StringType, ch.Config)
	data.Config = m
	return diags
}
