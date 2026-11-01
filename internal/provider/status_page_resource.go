package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

var _ resource.Resource = &StatusPageResource{}
var _ resource.ResourceWithImportState = &StatusPageResource{}
var _ resource.ResourceWithValidateConfig = &StatusPageResource{}

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
	Sections            types.List   `tfsdk:"sections"`
}

type statusPageSectionModel struct {
	Name       types.String `tfsdk:"name"`
	MonitorIDs types.List   `tfsdk:"monitor_ids"`
}

var statusPageSectionType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"name":        types.StringType,
	"monitor_ids": types.ListType{ElemType: types.StringType},
}}

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
				// OKS-906 — without it, any change left this unknown and Update sent false.
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_refresh_interval": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Seconds between automatic page refreshes (30-3600).",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"custom_domain": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"sections": schema.ListNestedAttribute{
				Optional: true,
				MarkdownDescription: "The monitors the page shows, grouped in named sections, in display order. " +
					"This is the complete list: it replaces the sections set in the dashboard, and `[]` empties the page. " +
					"Leave it out to manage the sections in the dashboard only; Terraform then never touches them. " +
					"A monitor can appear in one section only.",
				Validators: []validator.List{listvalidator.SizeAtMost(100)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "Section title shown on the page (1-100 characters).",
							Validators:          []validator.String{stringvalidator.LengthBetween(1, 100)},
						},
						"monitor_ids": schema.ListAttribute{
							Required:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "IDs of the monitors in this section, in display order.",
						},
					},
				},
			},
		},
	}
}

// ValidateConfig refuses a monitor listed twice: the API keeps it in the
// first section only, and Terraform would then report an inconsistent result
// after apply. Unknown ids (monitors created in the same apply) are skipped.
func (r *StatusPageResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var sections types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("sections"), &sections)...)
	if resp.Diagnostics.HasError() || !known(sections) {
		return
	}
	var items []statusPageSectionModel
	resp.Diagnostics.Append(sections.ElementsAs(ctx, &items, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	seen := map[string]int{}
	for i, s := range items {
		if !known(s.MonitorIDs) {
			continue
		}
		for _, v := range s.MonitorIDs.Elements() {
			id, ok := v.(types.String)
			if !ok || !known(id) {
				continue
			}
			if first, dup := seen[id.ValueString()]; dup {
				resp.Diagnostics.AddAttributeError(
					path.Root("sections").AtListIndex(i).AtName("monitor_ids"),
					"Monitor listed twice",
					fmt.Sprintf("Monitor %s is already in section %d. A monitor can appear in one section of a status page only.", id.ValueString(), first+1),
				)
				continue
			}
			seen[id.ValueString()] = i
		}
	}
}

func sectionsFromPlan(ctx context.Context, l types.List, diags *diag.Diagnostics) []client.StatusPageSection {
	var items []statusPageSectionModel
	diags.Append(l.ElementsAs(ctx, &items, false)...)
	out := make([]client.StatusPageSection, 0, len(items))
	for _, it := range items {
		ids := []string{}
		if known(it.MonitorIDs) {
			diags.Append(it.MonitorIDs.ElementsAs(ctx, &ids, false)...)
		}
		out = append(out, client.StatusPageSection{Name: it.Name.ValueString(), MonitorIDs: ids})
	}
	return out
}

func sectionsValue(ctx context.Context, sections []client.StatusPageSection, diags *diag.Diagnostics) types.List {
	items := make([]statusPageSectionModel, 0, len(sections))
	for _, s := range sections {
		ids := s.MonitorIDs
		if ids == nil {
			ids = []string{}
		}
		l, d := types.ListValueFrom(ctx, types.StringType, ids)
		diags.Append(d...)
		items = append(items, statusPageSectionModel{Name: types.StringValue(s.Name), MonitorIDs: l})
	}
	v, d := types.ListValueFrom(ctx, statusPageSectionType, items)
	diags.Append(d...)
	return v
}

// refreshSections puts the page's sections in state only when Terraform
// manages them (an import or a config without the attribute stays null, so
// sections edited in the dashboard never show a diff). sp.Sections nil = an
// API that does not return them yet: keep what state has.
func refreshSections(ctx context.Context, data *statusPageResourceModel, sp *client.StatusPage, diags *diag.Diagnostics) {
	if data.Sections.IsNull() || sp.Sections == nil {
		return
	}
	data.Sections = sectionsValue(ctx, *sp.Sections, diags)
}

// applySections sends the planned sections when Terraform manages them
// (attribute set) and returns the value to store in state.
func (r *StatusPageResource) applySections(ctx context.Context, id string, planned types.List, diags *diag.Diagnostics) types.List {
	if planned.IsNull() {
		return types.ListNull(statusPageSectionType)
	}
	in := sectionsFromPlan(ctx, planned, diags)
	if diags.HasError() {
		return planned
	}
	saved, err := r.client.SetStatusPageSections(ctx, id, in)
	if err != nil {
		diags.AddError("Unable to set status page sections", err.Error())
		return planned
	}
	return sectionsValue(ctx, saved, diags)
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

	planned := data.Sections
	writeStatusPage(&data, sp)
	// Save the page first: if the sections fail, it still exists in state and
	// the next apply retries the sections instead of creating a duplicate.
	data.Sections = types.ListNull(statusPageSectionType)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() || planned.IsNull() {
		return
	}
	data.Sections = r.applySections(ctx, sp.ID, planned, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
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
	refreshSections(ctx, &data, sp, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *StatusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior statusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sp, err := r.client.UpdateStatusPage(ctx, data.ID.ValueString(), statusPageUpdateInput(data))
	if err != nil {
		resp.Diagnostics.AddError("Unable to update status page", err.Error())
		return
	}

	planned := data.Sections
	writeStatusPage(&data, sp)
	switch {
	case planned.IsNull():
		// No longer managed by Terraform: leave the page's sections as they are.
		data.Sections = types.ListNull(statusPageSectionType)
	case planned.Equal(prior.Sections):
		data.Sections = planned
	default:
		data.Sections = r.applySections(ctx, sp.ID, planned, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// statusPageUpdateInput sends only known values (OKS-906): an unknown
// published or auto_refresh_interval used to go out as false / 0, which
// unpublished the page or got the PATCH refused (interval below 30).
func statusPageUpdateInput(data statusPageResourceModel) client.StatusPageUpdateInput {
	return client.StatusPageUpdateInput{
		Title:               stringPtrVal(data.Title),
		Description:         stringPtrOrNil(data.Description),
		LogoURL:             stringPtrOrNil(data.LogoURL),
		FaviconURL:          stringPtrOrNil(data.FaviconURL),
		AccentColor:         stringPtrOrNil(data.AccentColor),
		Theme:               stringPtrOrNil(data.Theme),
		CustomCSS:           stringPtrOrNil(data.CustomCSS),
		FooterText:          stringPtrOrNil(data.FooterText),
		Published:           boolPtrOrNil(data.Published),
		AutoRefreshInterval: int64PtrOrNil(data.AutoRefreshInterval),
		CustomDomain:        stringPtrOrNil(data.CustomDomain),
	}
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
