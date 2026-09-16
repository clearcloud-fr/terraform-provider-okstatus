package provider

import (
	"context"
	"regexp"

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

var _ resource.Resource = &StatusPageResource{}
var _ resource.ResourceWithImportState = &StatusPageResource{}

func NewStatusPageResource() resource.Resource {
	return &StatusPageResource{}
}

type StatusPageResource struct {
	client *client.Client
}

type statusPageResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Slug                types.String `tfsdk:"slug"`
	Title               types.String `tfsdk:"title"`
	Description         types.String `tfsdk:"description"`
	LogoURL             types.String `tfsdk:"logo_url"`
	FaviconURL          types.String `tfsdk:"favicon_url"`
	AccentColor         types.String `tfsdk:"accent_color"`
	Theme               types.String `tfsdk:"theme"`
	CustomCSS           types.String `tfsdk:"custom_css"`
	FooterText          types.String `tfsdk:"footer_text"`
	Published           types.Bool   `tfsdk:"published"`
	AutoRefreshInterval types.Int64  `tfsdk:"auto_refresh_interval"`
	CustomDomain        types.String `tfsdk:"custom_domain"`
}

func (r *StatusPageResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *StatusPageResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A public OKStatus status page.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"slug": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "URL slug (`status.okstatus.eu/<slug>`), lowercase letters/digits/hyphens. Immutable — changing it replaces the resource.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`), "must be lowercase letters, digits and hyphens"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"title": schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"logo_url":    schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"favicon_url": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"accent_color": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Hex color, e.g. `#00e599`. Defaults to OKStatus's own default.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^#[0-9a-fA-F]{6}$`), "must be a 6-digit hex color like #00e599"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"theme": schema.StringAttribute{
				Optional: true, Computed: true,
				Validators:    []validator.String{stringvalidator.OneOf("auto", "light", "dark")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"custom_css":  schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"footer_text": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"published": schema.BoolAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Whether the page is publicly visible. Defaults to `true`.",
			},
			"auto_refresh_interval": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Seconds between automatic page refreshes (30-3600).",
			},
			"custom_domain": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *StatusPageResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req.ProviderData, &resp.Diagnostics)
}

func (r *StatusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data statusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.StatusPageCreateInput{
		Slug:        data.Slug.ValueString(),
		Title:       data.Title.ValueString(),
		Description: stringPtrOrNil(data.Description),
		LogoURL:     stringPtrOrNil(data.LogoURL),
		FaviconURL:  stringPtrOrNil(data.FaviconURL),
		AccentColor: stringPtrOrNil(data.AccentColor),
		Theme:       stringPtrOrNil(data.Theme),
		CustomCSS:   stringPtrOrNil(data.CustomCSS),
		FooterText:  stringPtrOrNil(data.FooterText),
	}
	if !data.Published.IsNull() && !data.Published.IsUnknown() {
		v := data.Published.ValueBool()
		in.Published = &v
	}

	sp, err := r.client.CreateStatusPage(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create status page", err.Error())
		return
	}

	writeStatusPage(&data, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *StatusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data statusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sp, err := r.client.GetStatusPage(ctx, data.ID.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read status page", err.Error())
		return
	}

	writeStatusPage(&data, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *StatusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data statusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	published := data.Published.ValueBool()
	autoRefresh := data.AutoRefreshInterval.ValueInt64()
	sp, err := r.client.UpdateStatusPage(ctx, data.ID.ValueString(), client.StatusPageUpdateInput{
		Title:               stringPtrVal(data.Title),
		Description:         stringPtrOrNil(data.Description),
		LogoURL:             stringPtrOrNil(data.LogoURL),
		FaviconURL:          stringPtrOrNil(data.FaviconURL),
		AccentColor:         stringPtrOrNil(data.AccentColor),
		Theme:               stringPtrOrNil(data.Theme),
		CustomCSS:           stringPtrOrNil(data.CustomCSS),
		FooterText:          stringPtrOrNil(data.FooterText),
		Published:           &published,
		AutoRefreshInterval: &autoRefresh,
		CustomDomain:        stringPtrOrNil(data.CustomDomain),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update status page", err.Error())
		return
	}

	writeStatusPage(&data, sp)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *StatusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data statusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteStatusPage(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete status page", err.Error())
	}
}

func (r *StatusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func writeStatusPage(data *statusPageResourceModel, sp *client.StatusPage) {
	data.ID = types.StringValue(sp.ID)
	data.Slug = types.StringValue(sp.Slug)
	data.Title = types.StringValue(sp.Title)
	data.Description = nullableString(sp.Description)
	data.LogoURL = nullableString(sp.LogoURL)
	data.FaviconURL = nullableString(sp.FaviconURL)
	data.AccentColor = types.StringValue(sp.AccentColor)
	data.Theme = types.StringValue(sp.Theme)
	data.CustomCSS = nullableString(sp.CustomCSS)
	data.FooterText = nullableString(sp.FooterText)
	data.Published = types.BoolValue(sp.Published)
	data.AutoRefreshInterval = types.Int64Value(sp.AutoRefreshInterval)
	data.CustomDomain = nullableString(sp.CustomDomain)
}
