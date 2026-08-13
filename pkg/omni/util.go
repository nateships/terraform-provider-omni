// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// providerDataFromResource extracts the shared providerData set by the provider's Configure method.
//
// It returns nil (without adding a diagnostic) when data is nil, which happens during the
// framework's initial validation pass before the provider is configured.
func providerDataFromResource(in any, diags *diag.Diagnostics) *providerData {
	if in == nil {
		return nil
	}

	data, ok := in.(*providerData)
	if !ok {
		diags.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *providerData, got %T. This is a bug in the provider.", in),
		)

		return nil
	}

	return data
}

// errToDiag appends a Go error to the diagnostics as an error with the given summary.
func errToDiag(diags *diag.Diagnostics, summary string, err error) {
	diags.AddError(summary, err.Error())
}

// optionalString maps a server-side string onto an Optional (non-Computed) string attribute.
//
// The server reports the zero value for fields the user left unset, so the empty string is mapped
// back to null to avoid a permanent ""-vs-null diff against the configuration.
func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}

	return types.StringValue(value)
}

// optionalSet maps a server-side slice onto an Optional (non-Computed) set attribute.
//
// Like optionalString, an empty value maps back to null: the server reports nothing for a field the
// user left unset, and an empty set in state would diff forever against a null configuration.
func optionalSet(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if len(values) == 0 {
		return types.SetNull(types.StringType)
	}

	set, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)

	return set
}

// optionalList maps a server-side slice onto an Optional (non-Computed) list attribute.
func optionalList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}

	list, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)

	return list
}

// optionalMap maps a server-side map onto an Optional (non-Computed) map attribute.
func optionalMap(ctx context.Context, values map[string]string, diags *diag.Diagnostics) types.Map {
	if len(values) == 0 {
		return types.MapNull(types.StringType)
	}

	m, d := types.MapValueFrom(ctx, types.StringType, values)
	diags.Append(d...)

	return m
}

// reconcileDuration returns a string representation of serverDuration that round-trips cleanly
// against the value already in state.
//
// Durations are stored on the server as a normalized value, so the human-friendly form a user
// writes (e.g. "1h") differs textually from the server's form ("1h0m0s") even though they are
// equal. To avoid spurious diffs, the existing state value is preserved whenever it parses to the
// same duration as the server.
func reconcileDuration(existing types.String, serverDuration time.Duration) types.String {
	if serverDuration == 0 {
		return types.StringNull()
	}

	if !existing.IsNull() && !existing.IsUnknown() {
		if parsed, err := time.ParseDuration(existing.ValueString()); err == nil && parsed == serverDuration {
			return existing
		}
	}

	return types.StringValue(serverDuration.String())
}
