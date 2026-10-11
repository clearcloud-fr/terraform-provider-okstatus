package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ datasource.DataSource = &AlertChannelsDataSource{}

func NewAlertChannelsDataSource() datasource.DataSource {
	return &AlertChannelsDataSource{}
}

type AlertChannelsDataSource struct {
	client *client.Client
}

type alertChannelListItemModel struct {
	ID      types.String `tfsdk:"id"`
	Type    types.String `tfsdk:"type"`
	Name    types.String `tfsdk:"name"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

type alertChannelsDataSourceModel struct {
	ID       types.String                `tfsdk:"id"`
	Total    types.Int64                 `tfsdk:"total"`
	Channels []alertChannelListItemModel `tfsdk:"channels"`
}

func (d *AlertChannelsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channels"
}

func (d *AlertChannelsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every alert channel already configured in the org that owns the configured API key — " +
			"read-only, useful to check what exists before adding more. `config` (e.g. the webhook URL, which is " +
			"sensitive) is intentionally not exposed here — use `okstatus_alert_channel`'s own state if you manage " +
			"a channel as a resource and need its config back.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Placeholder identifier — this data source has no single ID of its own, required by the Terraform plugin framework.",
			},
			"total": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of alert channels currently configured in the org.",
			},
			"channels": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per alert channel.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":      schema.StringAttribute{Computed: true},
						"type":    schema.StringAttribute{Computed: true},
						"name":    schema.StringAttribute{Computed: true, MarkdownDescription: "The channel's name in the dashboard — the way to tell two channels of the same type apart."},
						"enabled": schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *AlertChannelsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *AlertChannelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	channels, err := d.client.ListAlertChannels(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list alert channels", err.Error())
		return
	}

	items := make([]alertChannelListItemModel, 0, len(channels))
	for _, ch := range channels {
		items = append(items, alertChannelListItemModel{
			ID:      types.StringValue(ch.ID),
			Type:    types.StringValue(ch.Type),
			Name:    nullableString(ch.Name),
			Enabled: types.BoolValue(ch.Enabled),
		})
	}

	data := alertChannelsDataSourceModel{
		// Fixed placeholder, not a real backend ID — this data source has no
		// single resource identity, only a framework-required `id`.
		ID:       types.StringValue("alert_channels"),
		Total:    types.Int64Value(int64(len(items))),
		Channels: items,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
