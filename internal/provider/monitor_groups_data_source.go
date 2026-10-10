package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ datasource.DataSource = &MonitorGroupsDataSource{}

func NewMonitorGroupsDataSource() datasource.DataSource {
	return &MonitorGroupsDataSource{}
}

type MonitorGroupsDataSource struct {
	client *client.Client
}

type monitorGroupListItemModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Color           types.String `tfsdk:"color"`
	Tags            types.Set    `tfsdk:"tags"`
	AlertChannelIDs types.Set    `tfsdk:"alert_channel_ids"`
	MonitorIDs      types.Set    `tfsdk:"monitor_ids"`
	Paused          types.Bool   `tfsdk:"paused"`
	Muted           types.Bool   `tfsdk:"muted"`
}

type monitorGroupsDataSourceModel struct {
	ID     types.String                `tfsdk:"id"`
	Total  types.Int64                 `tfsdk:"total"`
	Groups []monitorGroupListItemModel `tfsdk:"groups"`
}

func (d *MonitorGroupsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_groups"
}

func (d *MonitorGroupsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	stringSetAttr := func(description string) schema.SetAttribute {
		return schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: description}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every monitor group (the folders of the Monitors page) of the org that owns the " +
			"configured API key, in dashboard order, each with the ids of its monitors — read-only, e.g. to " +
			"find a group created in the dashboard by its name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Placeholder identifier — this data source has no single ID of its own, required by the Terraform plugin framework.",
			},
			"total": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of monitor groups in the org.",
			},
			"groups": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per group.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                schema.StringAttribute{Computed: true},
						"name":              schema.StringAttribute{Computed: true},
						"description":       schema.StringAttribute{Computed: true},
						"color":             schema.StringAttribute{Computed: true, MarkdownDescription: "Preset key of the dashboard's palette."},
						"tags":              stringSetAttr("The group's own tags."),
						"alert_channel_ids": stringSetAttr("Alert channels attached to any monitor that joins the group."),
						"monitor_ids":       stringSetAttr("Ids of the group's monitors."),
						"paused":            schema.BoolAttribute{Computed: true, MarkdownDescription: "The group was paused as a whole from the dashboard."},
						"muted":             schema.BoolAttribute{Computed: true, MarkdownDescription: "The group was muted as a whole from the dashboard."},
					},
				},
			},
		},
	}
}

func (d *MonitorGroupsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *MonitorGroupsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	groups, err := d.client.ListMonitorGroups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list monitor groups", err.Error())
		return
	}

	items := make([]monitorGroupListItemModel, 0, len(groups))
	for _, g := range groups {
		items = append(items, monitorGroupListItemModel{
			ID:              types.StringValue(g.ID),
			Name:            types.StringValue(g.Name),
			Description:     nullableString(g.Description),
			Color:           nullableString(g.Color),
			Tags:            stringSet(ctx, g.Tags, &resp.Diagnostics),
			AlertChannelIDs: stringSet(ctx, g.AlertChannelIDs, &resp.Diagnostics),
			MonitorIDs:      stringSet(ctx, g.MonitorIDs, &resp.Diagnostics),
			Paused:          types.BoolValue(g.Paused),
			Muted:           types.BoolValue(g.Muted),
		})
	}
	if resp.Diagnostics.HasError() {
		return
	}

	data := monitorGroupsDataSourceModel{
		ID:     types.StringValue("monitor_groups"),
		Total:  types.Int64Value(int64(len(items))),
		Groups: items,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
