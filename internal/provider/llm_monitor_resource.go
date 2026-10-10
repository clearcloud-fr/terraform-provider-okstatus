package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

// OKS-266 — LLM canary monitors (OKS-169 server-side): a scheduled prompt
// against an LLM endpoint with latency (TTFT) and content assertions.
// Modeled as its own resource rather than more optional attributes on
// okstatus_monitor: the field set barely overlaps and llm monitors carry
// their own rules (single region, api key write-only).

var _ resource.Resource = &LlmMonitorResource{}
var _ resource.ResourceWithImportState = &LlmMonitorResource{}

func NewLlmMonitorResource() resource.Resource {
	return &LlmMonitorResource{}
}

type LlmMonitorResource struct {
	client *client.Client
}

type llmMonitorResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	URL           types.String `tfsdk:"url"`
	LlmProvider   types.String `tfsdk:"llm_provider"`
	APIKey        types.String `tfsdk:"api_key"`
	Model         types.String `tfsdk:"model"`
	Prompt        types.String `tfsdk:"prompt"`
	Region        types.String `tfsdk:"region"`
	Interval      types.Int64  `tfsdk:"interval"`
	TtftMsMax     types.Int64  `tfsdk:"ttft_ms_max"`
	ContentEquals types.String `tfsdk:"content_equals"`
	ContentRegex  types.String `tfsdk:"content_regex"`
	Status        types.String `tfsdk:"status"`
}

func (r *LlmMonitorResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_llm_monitor"
}

func (r *LlmMonitorResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OKStatus LLM canary monitor: a scheduled prompt against an LLM " +
			"provider endpoint, asserting time-to-first-token and (optionally) the response content. " +
			"LLM monitors run from a single region — each region multiplies real token spend on your " +
			"provider account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Monitor ID.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name.",
			},
			"url": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The provider's chat-completions endpoint, e.g. " +
					"`https://api.openai.com/v1/chat/completions`.",
			},
			"llm_provider": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("openai-compatible"),
				MarkdownDescription: "Request/stream format: `openai-compatible` (OpenAI, Azure OpenAI, " +
					"Mistral, Groq, self-hosted vLLM/TGI, …), `anthropic`, or `bedrock`. Defaults to " +
					"`openai-compatible`.",
				Validators: []validator.String{
					stringvalidator.OneOf("openai-compatible", "anthropic", "bedrock"),
				},
			},
			"api_key": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				MarkdownDescription: "API key used by the canary checks against your provider. Write-only: " +
					"OKStatus masks it on every read, so the value in state is the one you configured.",
			},
			"model": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Model identifier, e.g. `gpt-4o-mini` or `claude-haiku-4-5`.",
			},
			"prompt": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("Reply with only the word: PONG"),
				MarkdownDescription: "Prompt sent on every check. Defaults to `Reply with only the word: PONG`.",
			},
			"region": schema.StringAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The single region the canary runs from (LLM monitors are " +
					"single-region by design). Defaults to `fr-dc3`.",
				Validators: []validator.String{
					regionValidator(),
				},
			},
			"interval": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Check interval in seconds (minimum 30). Defaults to 300.",
			},
			"ttft_ms_max": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Default:  int64default.StaticInt64(1500),
				MarkdownDescription: "Maximum acceptable time-to-first-token in milliseconds — slower " +
					"checks report `degraded`. Defaults to 1500.",
			},
			"content_equals": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Assert the full response equals exactly this string (trimmed).",
			},
			"content_regex": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Assert the full response matches this regular expression.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current monitor status as last reported by OKStatus (`pending`, `active`, `paused`, `down`).",
			},
		},
	}
}

func (r *LlmMonitorResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *LlmMonitorResource) llmConfigFromModel(data *llmMonitorResourceModel) *client.LlmConfig {
	cfg := &client.LlmConfig{
		Model:  data.Model.ValueString(),
		Prompt: data.Prompt.ValueString(),
		Timeouts: client.LlmTimeouts{
			ConnectMs:    5000,
			FirstTokenMs: 10000,
			TotalMs:      30000,
		},
		Assertions: client.LlmAssertions{
			TtftMsMax:     data.TtftMsMax.ValueInt64(),
			ContentEquals: data.ContentEquals.ValueString(),
			ContentRegex:  data.ContentRegex.ValueString(),
		},
	}
	return cfg
}

func (r *LlmMonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data llmMonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	region := "fr-dc3"
	if !data.Region.IsNull() && !data.Region.IsUnknown() {
		region = data.Region.ValueString()
	}

	in := client.MonitorCreateInput{
		Name:        data.Name.ValueString(),
		URL:         data.URL.ValueString(),
		Type:        "llm",
		Regions:     []string{region},
		LlmProvider: data.LlmProvider.ValueString(),
		LlmAPIKey:   data.APIKey.ValueString(),
		LlmConfig:   r.llmConfigFromModel(&data),
	}
	if !data.Interval.IsNull() && !data.Interval.IsUnknown() {
		in.Interval = data.Interval.ValueInt64()
	}

	m, err := r.client.CreateMonitor(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create LLM monitor", err.Error())
		return
	}

	writeLlmMonitor(&data, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LlmMonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data llmMonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.GetMonitor(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read LLM monitor", err.Error())
		return
	}

	writeLlmMonitor(&data, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LlmMonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data llmMonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// api_key comes back masked on reads, so the prior state never holds the
	// real key — always resend the configured value on update.
	var state llmMonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := data.Name.ValueString()
	url := data.URL.ValueString()
	in := client.MonitorUpdateInput{
		Name:        &name,
		URL:         &url,
		LlmProvider: data.LlmProvider.ValueString(),
		LlmAPIKey:   data.APIKey.ValueString(),
		LlmConfig:   r.llmConfigFromModel(&data),
	}
	if !data.Interval.IsNull() && !data.Interval.IsUnknown() {
		interval := data.Interval.ValueInt64()
		in.Interval = &interval
	}
	if !data.Region.IsNull() && !data.Region.IsUnknown() {
		in.Regions = []string{data.Region.ValueString()}
	}

	m, err := r.client.UpdateMonitor(ctx, data.ID.ValueString(), in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update LLM monitor", err.Error())
		return
	}

	writeLlmMonitor(&data, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *LlmMonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data llmMonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteMonitor(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete LLM monitor", err.Error())
	}
}

func (r *LlmMonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	// api_key can't be recovered from the API (masked on read) — the user
	// must set it in config after import; Terraform will then plan an update.
}

// writeLlmMonitor maps an API monitor onto the model. api_key is left
// untouched (the API masks it as "***" on every read — see sanitize-monitor
// server-side); content assertions and model/prompt come from llmConfig.
func writeLlmMonitor(data *llmMonitorResourceModel, m *client.Monitor) {
	data.ID = types.StringValue(m.ID)
	data.Name = types.StringValue(m.Name)
	data.URL = types.StringValue(m.URL)
	data.Interval = types.Int64Value(m.Interval)
	data.Status = types.StringValue(m.Status)
	if m.LlmProvider != "" {
		data.LlmProvider = types.StringValue(m.LlmProvider)
	}
	if len(m.Regions) > 0 {
		data.Region = types.StringValue(m.Regions[0])
	}
	if m.LlmConfig != nil {
		data.Model = types.StringValue(m.LlmConfig.Model)
		data.Prompt = types.StringValue(m.LlmConfig.Prompt)
		data.TtftMsMax = types.Int64Value(m.LlmConfig.Assertions.TtftMsMax)
		if m.LlmConfig.Assertions.ContentEquals != "" {
			data.ContentEquals = types.StringValue(m.LlmConfig.Assertions.ContentEquals)
		} else {
			data.ContentEquals = types.StringNull()
		}
		if m.LlmConfig.Assertions.ContentRegex != "" {
			data.ContentRegex = types.StringValue(m.LlmConfig.Assertions.ContentRegex)
		} else {
			data.ContentRegex = types.StringNull()
		}
	}
}
