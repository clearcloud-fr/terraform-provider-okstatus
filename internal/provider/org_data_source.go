package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ datasource.DataSource = &OrgDataSource{}

func NewOrgDataSource() datasource.DataSource {
	return &OrgDataSource{}
}

type OrgDataSource struct {
	client *client.Client
}

type orgDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Slug     types.String `tfsdk:"slug"`
	Plan     types.String `tfsdk:"plan"`
	Timezone types.String `tfsdk:"timezone"`
}

func (d *OrgDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org"
}

func (d *OrgDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The organization that owns the configured API key. Read-only.",
		Attributes: map[string]schema.Attribute{
			"id":       schema.StringAttribute{Computed: true},
			"name":     schema.StringAttribute{Computed: true},
			"slug":     schema.StringAttribute{Computed: true},
			"plan":     schema.StringAttribute{Computed: true},
			"timezone": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *OrgDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *client.Client.")
		return
	}
	d.client = c
}

func (d *OrgDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	org, err := d.client.GetOrg(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read organization", err.Error())
		return
	}

	data := orgDataSourceModel{
		ID:       types.StringValue(org.ID),
		Name:     types.StringValue(org.Name),
		Slug:     types.StringValue(org.Slug),
		Plan:     types.StringValue(org.Plan),
		Timezone: types.StringValue(org.Timezone),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
