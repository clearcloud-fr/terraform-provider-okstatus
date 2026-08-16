package provider

import (
	"fmt"

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
