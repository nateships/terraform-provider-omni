// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineClassSpecRoundTrip(t *testing.T) {
	r := &machineClassResource{}
	ctx := t.Context()

	for _, tc := range []struct {
		model machineClassResourceModel
		name  string
	}{
		{
			name: "match labels",
			model: machineClassResourceModel{
				Name:        types.StringValue("manual-pool"),
				MatchLabels: mustList(t, "amd64", "site = home"),
			},
		},
		{
			name: "auto provision",
			model: machineClassResourceModel{
				Name:        types.StringValue("proxmox-worker"),
				MatchLabels: types.ListNull(types.StringType),
				AutoProvision: &machineClassAutoProvisionModel{
					ProviderID:   types.StringValue("proxmox"),
					ProviderData: types.StringValue("cores: 8\nmemory: 16384\n"),
					KernelArgs:   mustList(t, "xe.force_probe=a780"),
					MetaValues: []machineClassMetaValueModel{
						{Key: types.Int64Value(0x0a), Value: types.StringValue("worker-{{ .MachineID }}")},
					},
					GRPCTunnel: types.BoolValue(true),
				},
			},
		},
		{
			name: "auto provision without optional fields",
			model: machineClassResourceModel{
				Name:        types.StringValue("bare"),
				MatchLabels: types.ListNull(types.StringType),
				AutoProvision: &machineClassAutoProvisionModel{
					ProviderID:   types.StringValue("bare-metal"),
					ProviderData: types.StringNull(),
					KernelArgs:   types.ListNull(types.StringType),
					GRPCTunnel:   types.BoolNull(),
				},
			},
		},
		{
			name: "auto provision with grpc tunnel disabled",
			model: machineClassResourceModel{
				Name:        types.StringValue("no-tunnel"),
				MatchLabels: types.ListNull(types.StringType),
				AutoProvision: &machineClassAutoProvisionModel{
					ProviderID:   types.StringValue("bare-metal"),
					ProviderData: types.StringNull(),
					KernelArgs:   types.ListNull(types.StringType),
					GRPCTunnel:   types.BoolValue(false),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics

			mc := omni.NewMachineClass(tc.model.Name.ValueString())

			r.applySpec(ctx, tc.model, mc, &diags)
			require.Empty(t, diags)

			var out machineClassResourceModel

			r.specToModel(ctx, mc, &out, &diags)
			require.Empty(t, diags)

			assert.Equal(t, tc.model, out)
		})
	}
}

func mustList(t *testing.T, elems ...string) types.List {
	t.Helper()

	list, diags := types.ListValueFrom(t.Context(), types.StringType, elems)
	require.Empty(t, diags)

	return list
}
