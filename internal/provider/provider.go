package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ provider.Provider = &OKStatusProvider{}

type OKStatusProvider struct {
	// version is "dev" for local builds, set to the real release version at
	// build time otherwise (see main.go).
	version string
}

// okstatusProviderModel mirrors the `okstatus { ... }` provider block.
type okstatusProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

func (p *OKStatusProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "okstatus"
	resp.Version = p.version
}

func (p *OKStatusProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage OKStatus (okstatus.eu) monitors, incidents, status pages, alert channels and maintenances as code.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "OKStatus API base URL. Defaults to `" + client.DefaultEndpoint + "`.",
				Optional:            true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "OKStatus API key (`oks_live_...`, from Dashboard → Settings → API Keys, Max plan required). Falls back to the `OKSTATUS_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *OKStatusProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data okstatusProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("OKSTATUS_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API Key",
			"Set the `api_key` provider attribute or the OKSTATUS_API_KEY environment variable. "+
				"Generate a key from Dashboard → Settings → API Keys (requires the Max plan).",
		)
		return
	}

	c := client.New(data.Endpoint.ValueString(), apiKey)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *OKStatusProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMonitorResource,
		NewIncidentResource,
		NewStatusPageResource,
		NewAlertChannelResource,
		NewMaintenanceResource,
	}
}

func (p *OKStatusProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrgDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &OKStatusProvider{version: version}
	}
}
