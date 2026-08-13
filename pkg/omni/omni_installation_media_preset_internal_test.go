// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/api/omni/management"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

const testImageFactoryURL = "https://factory.talos.dev"

// testStringSet builds a set attribute value, failing the test if the elements are not convertible.
func testStringSet(t *testing.T, values ...string) types.Set {
	t.Helper()

	set, diags := types.SetValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatalf("failed to build a set from %v: %v", values, diags)
	}

	return set
}

// testStringList builds a list attribute value, failing the test if the elements are not convertible.
func testStringList(t *testing.T, values ...string) types.List {
	t.Helper()

	list, diags := types.ListValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatalf("failed to build a list from %v: %v", values, diags)
	}

	return list
}

// testStringMap builds a map attribute value, failing the test if the elements are not convertible.
func testStringMap(t *testing.T, values map[string]string) types.Map {
	t.Helper()

	m, diags := types.MapValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatalf("failed to build a map from %v: %v", values, diags)
	}

	return m
}

// nullPresetModel is the baseline model: every optional attribute unset, with the two attributes
// that carry schema defaults holding those defaults.
func nullPresetModel() installationMediaPresetResourceModel {
	return installationMediaPresetResourceModel{
		Name:                  types.StringValue("preset"),
		Architecture:          types.StringValue(installationMediaArchAMD64),
		TalosVersion:          types.StringNull(),
		JoinToken:             types.StringNull(),
		Bootloader:            types.StringValue(installationMediaBootloaderAuto),
		EmbeddedMachineConfig: types.StringNull(),
		ImageFactoryURL:       types.StringValue(testImageFactoryURL),
		Extensions:            types.SetNull(types.StringType),
		KernelArgs:            types.ListNull(types.StringType),
		MachineLabels:         types.MapNull(types.StringType),
		GRPCTunnel:            types.BoolNull(),
		SecureBoot:            types.BoolValue(false),
	}
}

//nolint:maintidx
func TestInstallationMediaPresetApplyModel(t *testing.T) {
	r := &installationMediaPresetResource{}

	for _, tc := range []struct {
		expected    *specs.InstallationMediaConfigSpec
		model       func(*testing.T) installationMediaPresetResourceModel
		name        string
		expectedErr string
	}{
		{
			name: "bare metal defaults",
			model: func(*testing.T) installationMediaPresetResourceModel {
				return nullPresetModel()
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Bootloader:      management.SchematicBootloader_BOOT_AUTO,
				GrpcTunnel:      specs.GrpcTunnelMode_UNSET,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			name: "fully populated metal preset",
			model: func(t *testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Architecture = types.StringValue(installationMediaArchARM64)
				model.TalosVersion = types.StringValue("1.13.5")
				model.JoinToken = types.StringValue("token-id")
				model.Bootloader = types.StringValue(installationMediaBootloaderDual)
				model.EmbeddedMachineConfig = types.StringValue("version: v1alpha1\n")
				model.Extensions = testStringSet(t, "siderolabs/qemu-guest-agent", "siderolabs/intel-ucode")
				model.KernelArgs = testStringList(t, "console=ttyS0", "talos.platform=metal")
				model.MachineLabels = testStringMap(t, map[string]string{"env": "production", "team": "infra"})
				model.GRPCTunnel = types.BoolValue(true)
				model.SecureBoot = types.BoolValue(true)

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				TalosVersion:          "1.13.5",
				Architecture:          specs.PlatformConfigSpec_ARM64,
				InstallExtensions:     []string{"siderolabs/qemu-guest-agent", "siderolabs/intel-ucode"},
				KernelArgs:            "console=ttyS0 talos.platform=metal",
				JoinToken:             "token-id",
				SecureBoot:            true,
				GrpcTunnel:            specs.GrpcTunnelMode_ENABLED,
				MachineLabels:         map[string]string{"env": "production", "team": "infra"},
				Bootloader:            management.SchematicBootloader_BOOT_DUAL,
				EmbeddedMachineConfig: "version: v1alpha1\n",
				ImageFactoryUrl:       testImageFactoryURL,
			},
		},
		{
			name: "cloud preset",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Cloud = &installationMediaPresetCloudModel{Platform: types.StringValue("aws")}

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Cloud:           &specs.InstallationMediaConfigSpec_Cloud{Platform: "aws"},
				Bootloader:      management.SchematicBootloader_BOOT_AUTO,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			name: "sbc preset with overlay options",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Architecture = types.StringValue(installationMediaArchARM64)
				model.SBC = &installationMediaPresetSBCModel{
					Overlay:        types.StringValue("rpi_generic"),
					OverlayOptions: types.StringValue("configTxt: dGVzdA==\n"),
				}

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture: specs.PlatformConfigSpec_ARM64,
				Sbc: &specs.InstallationMediaConfigSpec_SBC{
					Overlay:        "rpi_generic",
					OverlayOptions: "configTxt: dGVzdA==\n",
				},
				Bootloader:      management.SchematicBootloader_BOOT_AUTO,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			// Omni stores the canonical "1.2.3" form, so the leading "v" a user may write is stripped
			// before the value is sent, keeping the value that comes back identical.
			name: "leading v is stripped from the Talos version",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.TalosVersion = types.StringValue("v1.13.5")

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				TalosVersion:    "1.13.5",
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Bootloader:      management.SchematicBootloader_BOOT_AUTO,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			// The names an operator uses are not the enum's own: uefi selects systemd-boot.
			name: "uefi selects the systemd-boot bootloader",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Bootloader = types.StringValue(installationMediaBootloaderUEFI)

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Bootloader:      management.SchematicBootloader_BOOT_SD,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			name: "bios selects the GRUB bootloader",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Bootloader = types.StringValue(installationMediaBootloaderBIOS)

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Bootloader:      management.SchematicBootloader_BOOT_GRUB,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			// An explicit false is not the same as leaving the attribute unset: it pins the tunnel off,
			// where an unset attribute lets Omni decide at download time.
			name: "grpc_tunnel false is stored as disabled",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.GRPCTunnel = types.BoolValue(false)

				return model
			},
			expected: &specs.InstallationMediaConfigSpec{
				Architecture:    specs.PlatformConfigSpec_AMD64,
				Bootloader:      management.SchematicBootloader_BOOT_AUTO,
				GrpcTunnel:      specs.GrpcTunnelMode_DISABLED,
				ImageFactoryUrl: testImageFactoryURL,
			},
		},
		{
			name: "cloud and sbc together are rejected",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Cloud = &installationMediaPresetCloudModel{Platform: types.StringValue("aws")}
				model.SBC = &installationMediaPresetSBCModel{Overlay: types.StringValue("rpi_generic")}

				return model
			},
			expectedErr: "cloud and sbc are mutually exclusive: a preset targets a cloud platform, an SBC overlay, or bare metal.",
		},
		{
			// Joining an argument that carries whitespace would split it back into several on read,
			// diffing forever against the configuration.
			name: "kernel argument containing whitespace is rejected",
			model: func(t *testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.KernelArgs = testStringList(t, "console=ttyS0", "talos.platform=metal init_on_alloc=1")

				return model
			},
			expectedErr: "Kernel argument \"talos.platform=metal init_on_alloc=1\" contains whitespace. Kernel arguments are " +
				"stored space-separated, so each element must be a single argument.",
		},
		{
			name: "unknown architecture is rejected",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Architecture = types.StringValue("riscv64")

				return model
			},
			expectedErr: "Unsupported architecture \"riscv64\", must be one of: amd64, arm64.",
		},
		{
			name: "unknown bootloader is rejected",
			model: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Bootloader = types.StringValue("uefii")

				return model
			},
			expectedErr: "Unknown bootloader \"uefii\", must be one of: auto, uefi, bios, dual.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics

			preset := omni.NewInstallationMediaConfig("preset")

			r.applyInstallationMediaPresetModel(context.Background(), tc.model(t), testImageFactoryURL, preset, &diags)

			if tc.expectedErr != "" {
				if !diags.HasError() {
					t.Fatalf("applyInstallationMediaPresetModel() produced no error, want %q", tc.expectedErr)
				}

				if detail := diags.Errors()[0].Detail(); detail != tc.expectedErr {
					t.Fatalf("applyInstallationMediaPresetModel() error = %q, want %q", detail, tc.expectedErr)
				}

				return
			}

			if diags.HasError() {
				t.Fatalf("applyInstallationMediaPresetModel() returned unexpected diagnostics: %v", diags)
			}

			if got := preset.TypedSpec().Value; !got.EqualVT(tc.expected) {
				t.Fatalf("spec = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestInstallationMediaPresetToModel(t *testing.T) {
	r := &installationMediaPresetResource{}

	t.Run("all fields set", func(t *testing.T) {
		preset := omni.NewInstallationMediaConfig("full")
		preset.TypedSpec().Value = &specs.InstallationMediaConfigSpec{
			TalosVersion:          "1.13.5",
			Architecture:          specs.PlatformConfigSpec_ARM64,
			InstallExtensions:     []string{"siderolabs/qemu-guest-agent"},
			KernelArgs:            "console=ttyS0 talos.platform=metal",
			Sbc:                   &specs.InstallationMediaConfigSpec_SBC{Overlay: "rpi_generic", OverlayOptions: "opts"},
			JoinToken:             "token-id",
			SecureBoot:            true,
			GrpcTunnel:            specs.GrpcTunnelMode_ENABLED,
			MachineLabels:         map[string]string{"env": "production"},
			Bootloader:            management.SchematicBootloader_BOOT_SD,
			EmbeddedMachineConfig: "version: v1alpha1\n",
			ImageFactoryUrl:       testImageFactoryURL,
		}

		var (
			model installationMediaPresetResourceModel
			diags diag.Diagnostics
		)

		r.installationMediaPresetToModel(context.Background(), preset, &model, &diags)

		if diags.HasError() {
			t.Fatalf("installationMediaPresetToModel() returned unexpected diagnostics: %v", diags)
		}

		expected := installationMediaPresetResourceModel{
			SBC: &installationMediaPresetSBCModel{
				Overlay:        types.StringValue("rpi_generic"),
				OverlayOptions: types.StringValue("opts"),
			},
			Name:                  types.StringValue("full"),
			Architecture:          types.StringValue(installationMediaArchARM64),
			TalosVersion:          types.StringValue("1.13.5"),
			JoinToken:             types.StringValue("token-id"),
			Bootloader:            types.StringValue(installationMediaBootloaderUEFI),
			EmbeddedMachineConfig: types.StringValue("version: v1alpha1\n"),
			ImageFactoryURL:       types.StringValue(testImageFactoryURL),
			Extensions:            testStringSet(t, "siderolabs/qemu-guest-agent"),
			KernelArgs:            testStringList(t, "console=ttyS0", "talos.platform=metal"),
			MachineLabels:         testStringMap(t, map[string]string{"env": "production"}),
			GRPCTunnel:            types.BoolValue(true),
			SecureBoot:            types.BoolValue(true),
		}

		if !reflect.DeepEqual(model, expected) {
			t.Fatalf("model = %+v, want %+v", model, expected)
		}
	})

	// Unset optional fields come back from the server as zero values; they must map to null so the
	// state matches a configuration that omits them.
	t.Run("unset optional fields are null", func(t *testing.T) {
		preset := omni.NewInstallationMediaConfig("empty")
		preset.TypedSpec().Value = &specs.InstallationMediaConfigSpec{
			Architecture: specs.PlatformConfigSpec_AMD64,
		}

		model := installationMediaPresetResourceModel{
			Cloud:                 &installationMediaPresetCloudModel{Platform: types.StringValue("stale")},
			SBC:                   &installationMediaPresetSBCModel{Overlay: types.StringValue("stale")},
			TalosVersion:          types.StringValue("stale"),
			JoinToken:             types.StringValue("stale"),
			EmbeddedMachineConfig: types.StringValue("stale"),
			Extensions:            testStringSet(t, "stale"),
			KernelArgs:            testStringList(t, "stale"),
			MachineLabels:         testStringMap(t, map[string]string{"stale": "stale"}),
			GRPCTunnel:            types.BoolValue(true),
		}

		var diags diag.Diagnostics

		r.installationMediaPresetToModel(context.Background(), preset, &model, &diags)

		if diags.HasError() {
			t.Fatalf("installationMediaPresetToModel() returned unexpected diagnostics: %v", diags)
		}

		if model.Cloud != nil {
			t.Fatalf("cloud = %+v, want nil", model.Cloud)
		}

		if model.SBC != nil {
			t.Fatalf("sbc = %+v, want nil", model.SBC)
		}

		for name, got := range map[string]interface{ IsNull() bool }{
			"talos_version":           model.TalosVersion,
			"join_token":              model.JoinToken,
			"embedded_machine_config": model.EmbeddedMachineConfig,
			"extensions":              model.Extensions,
			"kernel_args":             model.KernelArgs,
			"machine_labels":          model.MachineLabels,
			"grpc_tunnel":             model.GRPCTunnel,
		} {
			if !got.IsNull() {
				t.Fatalf("%s = %v, want null", name, got)
			}
		}
	})
}

// TestInstallationMediaPresetRoundTrip asserts that a model survives a write to the spec and a read
// back, which is what keeps `terraform plan` empty after an apply.
func TestInstallationMediaPresetRoundTrip(t *testing.T) {
	r := &installationMediaPresetResource{}

	for _, tc := range []struct {
		plan func(*testing.T) installationMediaPresetResourceModel
		name string
	}{
		{
			name: "bare metal defaults",
			plan: func(*testing.T) installationMediaPresetResourceModel {
				return nullPresetModel()
			},
		},
		{
			name: "fully populated metal preset",
			plan: func(t *testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Architecture = types.StringValue(installationMediaArchARM64)
				model.TalosVersion = types.StringValue("1.13.5")
				model.JoinToken = types.StringValue("token-id")
				model.Bootloader = types.StringValue(installationMediaBootloaderBIOS)
				model.EmbeddedMachineConfig = types.StringValue("version: v1alpha1\n")
				model.Extensions = testStringSet(t, "siderolabs/qemu-guest-agent")
				model.KernelArgs = testStringList(t, "console=ttyS0", "talos.platform=metal")
				model.MachineLabels = testStringMap(t, map[string]string{"env": "production"})
				model.GRPCTunnel = types.BoolValue(true)
				model.SecureBoot = types.BoolValue(true)

				return model
			},
		},
		{
			name: "cloud preset",
			plan: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Cloud = &installationMediaPresetCloudModel{Platform: types.StringValue("aws")}

				return model
			},
		},
		{
			name: "sbc preset without overlay options",
			plan: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.Architecture = types.StringValue(installationMediaArchARM64)
				model.SBC = &installationMediaPresetSBCModel{
					Overlay:        types.StringValue("rpi_generic"),
					OverlayOptions: types.StringNull(),
				}

				return model
			},
		},
		{
			name: "grpc tunnel pinned off",
			plan: func(*testing.T) installationMediaPresetResourceModel {
				model := nullPresetModel()
				model.GRPCTunnel = types.BoolValue(false)

				return model
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics

			plan := tc.plan(t)

			preset := omni.NewInstallationMediaConfig(plan.Name.ValueString())

			r.applyInstallationMediaPresetModel(context.Background(), plan, testImageFactoryURL, preset, &diags)

			if diags.HasError() {
				t.Fatalf("applyInstallationMediaPresetModel() returned unexpected diagnostics: %v", diags)
			}

			var readBack installationMediaPresetResourceModel

			r.installationMediaPresetToModel(context.Background(), preset, &readBack, &diags)

			if diags.HasError() {
				t.Fatalf("installationMediaPresetToModel() returned unexpected diagnostics: %v", diags)
			}

			if !reflect.DeepEqual(readBack, plan) {
				t.Fatalf("round trip = %+v, want %+v", readBack, plan)
			}
		})
	}
}

func TestNormalizeImageFactoryURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "trailing slash is stripped", in: "https://factory.talos.dev/", want: "https://factory.talos.dev"},
		{name: "canonical URL is unchanged", in: "https://factory.talos.dev", want: "https://factory.talos.dev"},
		{name: "empty stays empty", in: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeImageFactoryURL(tc.in); got != tc.want {
				t.Fatalf("normalizeImageFactoryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
