package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ datasource.DataSource = &PrivateAgentsDataSource{}

func NewPrivateAgentsDataSource() datasource.DataSource {
	return &PrivateAgentsDataSource{}
}

type PrivateAgentsDataSource struct {
	client *client.Client
}

type privateAgentListItemModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Site            types.String `tfsdk:"site"`
	Region          types.String `tfsdk:"region"`
	Status          types.String `tfsdk:"status"`
	Suspended       types.Bool   `tfsdk:"suspended"`
	Version         types.String `tfsdk:"version"`
	Hostname        types.String `tfsdk:"hostname"`
	Platform        types.String `tfsdk:"platform"`
	LastHeartbeatAt types.String `tfsdk:"last_heartbeat_at"`
}

type privateAgentsDataSourceModel struct {
	ID            types.String                `tfsdk:"id"`
	Total         types.Int64                 `tfsdk:"total"`
	Eligible      types.Bool                  `tfsdk:"eligible"`
	Limit         types.Int64                 `tfsdk:"limit"`
	LatestVersion types.String                `tfsdk:"latest_version"`
	Agents        []privateAgentListItemModel `tfsdk:"agents"`
}

func (d *PrivateAgentsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_private_agents"
}

func (d *PrivateAgentsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The private agents of the org that owns the configured API key — the probes you " +
			"run on your own network. Read-only: an agent is enrolled from the dashboard, since enrolling " +
			"hands out a credential. Use an agent's `region` in the `regions` of an `okstatus_monitor` to run " +
			"that monitor on it. Enrollment tokens and credentials are never returned.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Placeholder identifier — this data source has no single ID of its own, required by the Terraform plugin framework.",
			},
			"total": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of private agents in the org.",
			},
			"eligible": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the org's plan includes private agents.",
			},
			"limit": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Number of private agents the plan allows.",
			},
			"latest_version": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Latest published version of the agent, to compare with each agent's `version`.",
			},
			"agents": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per agent.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.StringAttribute{Computed: true},
						"name": schema.StringAttribute{Computed: true},
						"site": schema.StringAttribute{Computed: true, MarkdownDescription: "Where the agent runs, as entered at enrollment."},
						"region": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Value to put in a monitor's `regions` to run it on this agent (`private:<id>`).",
						},
						"status": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "`up`, `degraded`, `offline`, `suspended` or `pending` (not enrolled yet).",
						},
						"suspended":         schema.BoolAttribute{Computed: true},
						"version":           schema.StringAttribute{Computed: true},
						"hostname":          schema.StringAttribute{Computed: true},
						"platform":          schema.StringAttribute{Computed: true},
						"last_heartbeat_at": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *PrivateAgentsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (d *PrivateAgentsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	list, err := d.client.ListPrivateAgents(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list private agents", err.Error())
		return
	}

	items := make([]privateAgentListItemModel, 0, len(list.Agents))
	for _, a := range list.Agents {
		items = append(items, privateAgentListItemModel{
			ID:              types.StringValue(a.ID),
			Name:            types.StringValue(a.Name),
			Site:            nullableString(a.Site),
			Region:          types.StringValue(a.Region),
			Status:          types.StringValue(a.Status),
			Suspended:       types.BoolValue(a.Suspended),
			Version:         nullableString(a.Version),
			Hostname:        nullableString(a.Hostname),
			Platform:        nullableString(a.Platform),
			LastHeartbeatAt: nullableString(a.LastHeartbeatAt),
		})
	}

	data := privateAgentsDataSourceModel{
		ID:            types.StringValue("private_agents"),
		Total:         types.Int64Value(int64(len(items))),
		Eligible:      types.BoolValue(list.Meta.Eligible),
		Limit:         types.Int64Value(list.Meta.Limit),
		LatestVersion: nullableString(list.Meta.LatestVersion),
		Agents:        items,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
