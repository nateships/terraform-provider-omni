// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/siderolabs/omni/client/api/omni/management"
	"github.com/siderolabs/omni/client/api/omni/specs"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"

	"github.com/siderolabs/terraform-provider-omni/pkg/omni"
)

func TestAccOmniInstallationMediaPresetResource(t *testing.T) {
	name := "tf-acc-preset"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationMediaPresetDestroy,
		Steps: []resource.TestStep{
			{ // create a bare-metal preset
				Config: testAccInstallationMediaPresetConfig(name, `
  bootloader  = "uefi"
  secure_boot = true

  kernel_args = ["console=ttyS0"]

  machine_labels = {
    env = "tf-acc"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "name", name),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "architecture", "amd64"),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "bootloader", "uefi"),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "secure_boot", "true"),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "kernel_args.0", "console=ttyS0"),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "machine_labels.env", "tf-acc"),
					resource.TestCheckNoResourceAttr("omni_installation_media_preset.test", "talos_version"),
					resource.TestCheckNoResourceAttr("omni_installation_media_preset.test", "cloud"),
					resource.TestCheckNoResourceAttr("omni_installation_media_preset.test", "sbc"),
					// The factory URL is never configured by the user: it is derived from the instance.
					resource.TestCheckResourceAttrSet("omni_installation_media_preset.test", "image_factory_url"),
					testAccCheckInstallationMediaPreset(name, func(spec *specs.InstallationMediaConfigSpec) error {
						if spec.GetArchitecture() != specs.PlatformConfigSpec_AMD64 {
							return fmt.Errorf("unexpected architecture: %v", spec.GetArchitecture())
						}

						if spec.GetBootloader() != management.SchematicBootloader_BOOT_SD {
							return fmt.Errorf("unexpected bootloader: %v", spec.GetBootloader())
						}

						if !spec.GetSecureBoot() {
							return fmt.Errorf("secure boot is not enabled")
						}

						if spec.GetKernelArgs() != "console=ttyS0" {
							return fmt.Errorf("unexpected kernel args: %q", spec.GetKernelArgs())
						}

						if spec.GetMachineLabels()["env"] != "tf-acc" {
							return fmt.Errorf("unexpected machine labels: %v", spec.GetMachineLabels())
						}

						return nil
					}),
					testAccCheckInstallationMediaPresetFactory(name),
				),
			},
			{ // presets are imported by name
				ResourceName:                         "omni_installation_media_preset.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
			{ // update in place: swap the labels and drop the kernel args
				Config: testAccInstallationMediaPresetConfig(name, `
  bootloader  = "uefi"
  secure_boot = true

  machine_labels = {
    env  = "tf-acc-updated"
    team = "infra"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "machine_labels.env", "tf-acc-updated"),
					resource.TestCheckResourceAttr("omni_installation_media_preset.test", "machine_labels.team", "infra"),
					resource.TestCheckNoResourceAttr("omni_installation_media_preset.test", "kernel_args"),
					testAccCheckInstallationMediaPreset(name, func(spec *specs.InstallationMediaConfigSpec) error {
						if spec.GetKernelArgs() != "" {
							return fmt.Errorf("kernel args were not cleared: %q", spec.GetKernelArgs())
						}

						if spec.GetMachineLabels()["env"] != "tf-acc-updated" {
							return fmt.Errorf("unexpected machine labels: %v", spec.GetMachineLabels())
						}

						return nil
					}),
				),
			},
		},
	})
}

// TestAccOmniInstallationMediaPresetResourceConflictingTargets asserts that a preset targeting both
// a cloud platform and an SBC overlay is rejected at plan time, before anything reaches Omni.
func TestAccOmniInstallationMediaPresetResourceConflictingTargets(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstallationMediaPresetConfig("tf-acc-preset-conflicting", `
  cloud = {
    platform = "aws"
  }

  sbc = {
    overlay = "rpi_generic"
  }
`),
				ExpectError: regexp.MustCompile("Invalid Attribute Combination"),
			},
		},
	})
}

// TestAccOmniInstallationMediaPresetResourceKernelArgWithWhitespace asserts that a kernel argument
// carrying whitespace is rejected at plan time. Such an element would be joined into the
// space-separated string Omni stores and split back into several arguments on read, so the resource
// would never converge.
func TestAccOmniInstallationMediaPresetResourceKernelArgWithWhitespace(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccInstallationMediaPresetConfig("tf-acc-preset-bad-args", "  kernel_args = [\"console=ttyS0 init_on_alloc=1\"]\n"),
				// Terraform hard-wraps a diagnostic's detail text, so the pattern has to tolerate a line
				// break wherever a space appears.
				ExpectError: regexp.MustCompile(`must\s+be\s+a\s+single\s+kernel\s+argument`),
			},
		},
	})
}

// TestAccOmniInstallationMediaPresetResourceUnknownTalosVersion asserts that Omni's own validation
// surfaces as a resource error rather than a silently broken preset.
func TestAccOmniInstallationMediaPresetResourceUnknownTalosVersion(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckInstallationMediaPresetDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccInstallationMediaPresetConfig("tf-acc-preset-bad-version", "  talos_version = \"0.0.1\"\n"),
				ExpectError: regexp.MustCompile("Failed to create Omni installation media preset"),
			},
		},
	})
}

func testAccInstallationMediaPresetConfig(name, extra string) string {
	return fmt.Sprintf(`
provider "omni" {
  insecure_skip_tls_verify = true
}

resource "omni_installation_media_preset" "test" {
  name = %q
%s}
`, name, extra)
}

// testAccCheckInstallationMediaPreset reads the preset back over the live Omni API and hands its
// spec to the caller's assertions.
func testAccCheckInstallationMediaPreset(name string, check func(*specs.InstallationMediaConfigSpec) error) resource.TestCheckFunc {
	return func(*terraform.State) error {
		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		preset, err := safe.ReaderGetByID[*omnires.InstallationMediaConfig](context.Background(), client.Omni().State(), name)
		if err != nil {
			return fmt.Errorf("failed to read installation media preset %q: %w", name, err)
		}

		return check(preset.TypedSpec().Value)
	}
}

// testAccCheckInstallationMediaPresetFactory asserts that the provider pinned the preset to a
// factory the instance actually has configured, the same check Omni performs on write.
func testAccCheckInstallationMediaPresetFactory(name string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		ctx := context.Background()

		preset, err := safe.ReaderGetByID[*omnires.InstallationMediaConfig](ctx, client.Omni().State(), name)
		if err != nil {
			return fmt.Errorf("failed to read installation media preset %q: %w", name, err)
		}

		factoryURL := preset.TypedSpec().Value.GetImageFactoryUrl()
		if factoryURL == "" {
			return fmt.Errorf("installation media preset %q has no image factory URL", name)
		}

		features, err := safe.ReaderGetByID[*omnires.FeaturesConfig](ctx, client.Omni().State(), omnires.FeaturesConfigID)
		if err != nil {
			return fmt.Errorf("failed to read the features config: %w", err)
		}

		configured := []string{
			strings.TrimRight(features.TypedSpec().Value.GetImageFactoryBaseUrl(), "/"),
			strings.TrimRight(features.TypedSpec().Value.GetSecondaryImageFactoryBaseUrl(), "/"),
		}

		for _, url := range configured {
			if url != "" && url == factoryURL {
				return nil
			}
		}

		return fmt.Errorf("image factory %q is not one of the configured factories %v", factoryURL, configured)
	}
}

// testAccCheckInstallationMediaPresetDestroy asserts, via the live Omni API, that every preset the
// test managed is gone.
func testAccCheckInstallationMediaPresetDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "omni_installation_media_preset" {
			continue
		}

		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		name := rs.Primary.Attributes["name"]

		_, err = safe.ReaderGetByID[*omnires.InstallationMediaConfig](context.Background(), client.Omni().State(), name)
		if err == nil {
			return fmt.Errorf("installation media preset %q still exists", name)
		}

		if !cosistate.IsNotFoundError(err) {
			return fmt.Errorf("unexpected error checking installation media preset %q: %w", name, err)
		}
	}

	return nil
}
