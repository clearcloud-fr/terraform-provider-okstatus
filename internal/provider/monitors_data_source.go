package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ datasource.DataSource = &MonitorsDataSource{}

func NewMonitorsDataSource() datasource.DataSource {
	return &MonitorsDataSource{}
}

type MonitorsDataSource struct {
	client *client.Client
}

type monitorListItemModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	URL      types.String `tfsdk:"url"`
	Type     types.String `tfsdk:"type"`
	Interval types.Int64  `tfsdk:"interval"`
	Status   types.String `tfsdk:"status"`
}

type monitorsDataSourceModel struct {
	ID       types.String           `tfsdk:"id"`
	Total    types.Int64            `tfsdk:"total"`
	Monitors []monitorListItemModel `tfsdk:"monitors"`
}

func (d *MonitorsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitors"
}

func (d *MonitorsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every monitor already configured in the org that owns the configured API key — " +
			"read-only, useful to check what exists before adding more (e.g. `data.okstatus_monitors.all.total`, " +
			"or `length(data.okstatus_monitors.all.monitors)`). Pages through `GET /monitors` internally, so " +
			"`total`/`monitors` reflect the full set regardless of the API's per-request page cap.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Placeholder identifier — this data source has no single ID of its own, required by the Terraform plugin framework.",
			},
			"total": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of monitors currently configured in the org.",
			},
			"monitors": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per monitor.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":       schema.StringAttribute{Computed: true},
						"name":     schema.StringAttribute{Computed: true},
						"url":      schema.StringAttribute{Computed: true},
						"type":     schema.StringAttribute{Computed: true},
						"interval": schema.Int64Attribute{Computed: true},
						"status":   schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *MonitorsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *MonitorsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	monitors, err := d.client.ListMonitors(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list monitors", err.Error())
		return
	}

	items := make([]monitorListItemModel, 0, len(monitors))
	for _, m := range monitors {
		items = append(items, monitorListItemModel{
			ID:       types.StringValue(m.ID),
			Name:     types.StringValue(m.Name),
			URL:      types.StringValue(m.URL),
			Type:     types.StringValue(m.Type),
			Interval: types.Int64Value(m.Interval),
			Status:   types.StringValue(m.Status),
		})
	}

	data := monitorsDataSourceModel{
		// Fixed placeholder, not a real backend ID — this data source has no
		// single resource identity, only a framework-required `id`.
		ID:       types.StringValue("monitors"),
		Total:    types.Int64Value(int64(len(items))),
		Monitors: items,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
