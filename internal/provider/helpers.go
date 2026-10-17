package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Clearcloud-Fr/terraform-provider-okstatus/internal/client"
)

// configureClient is the shared Configure() body every resource/data source
// needs: pull the *client.Client set by provider.Configure out of
// req.ProviderData, with the same nil-check/type-assert dance the official
// scaffolding template uses.
func configureClient(providerData any, diags *diag.Diagnostics) *client.Client {
	if providerData == nil {
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", providerData),
		)
		return nil
	}
	return c
}

// nullableString converts an optional API string pointer into a
// types.String — null when the pointer is nil, matching how the framework
// represents "attribute absent" for Optional/Computed string attributes.
func nullableString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func stringPtrOrNil(s types.String) *string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	v := s.ValueString()
	return &v
}

// stringPtrVal is for Required (never-null) string attributes where the API
// always wants the field sent, unlike stringPtrOrNil's Optional handling.
func stringPtrVal(s types.String) *string {
	v := s.ValueString()
	return &v
}

// known reports whether a planned value was actually decided — neither left
// out of the configuration (null) nor still to be computed (unknown).
func known(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func boolPtrOrNil(b types.Bool) *bool {
	if !known(b) {
		return nil
	}
	v := b.ValueBool()
	return &v
}

func float64PtrOrNil(f types.Float64) *float64 {
	if !known(f) {
		return nil
	}
	v := f.ValueFloat64()
	return &v
}

func int64PtrOrNil(i types.Int64) *int64 {
	if !known(i) {
		return nil
	}
	v := i.ValueInt64()
	return &v
}

// stringOrEmpty maps an absent API string to "" (see writeMonitor).
func stringOrEmpty(s *string) types.String {
	if s == nil {
		return types.StringValue("")
	}
	return types.StringValue(*s)
}
