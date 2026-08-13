# Bare-metal media, with extra kernel args and labels applied to every machine that joins from it.
# The preset does not fix a file format: the same preset serves an ISO, a raw or qcow2 disk image or
# a PXE boot URL, picked per download.
resource "omni_installation_media_preset" "metal" {
  name          = "metal-production"
  architecture  = "amd64"
  talos_version = "1.13.5"

  extensions = [
    "siderolabs/qemu-guest-agent",
    "siderolabs/intel-ucode",
  ]

  kernel_args = ["console=ttyS0"]

  machine_labels = {
    env  = "production"
    team = "infra"
  }
}

# Cloud media: the image format follows the platform, so there is nothing to choose at download time.
resource "omni_installation_media_preset" "aws" {
  name         = "aws-production"
  architecture = "amd64"

  cloud = {
    platform = "aws"
  }

  machine_labels = {
    env = "production"
  }
}

# Single-board-computer media: always a raw disk image, built with the overlay for the board.
resource "omni_installation_media_preset" "rpi" {
  name         = "rpi-generic"
  architecture = "arm64"

  sbc = {
    overlay = "rpi_generic"
  }
}

# A preset carrying a machine configuration embedded into the media itself.
resource "omni_installation_media_preset" "embedded" {
  name         = "metal-embedded"
  architecture = "amd64"

  embedded_machine_config = file("${path.module}/machine-config.yaml")
}

# Download media from a preset. The format applies to bare-metal presets only; cloud and SBC presets
# reject it, since their format is implied.
#
#   omnictl media download metal-production --output ./              # ISO (the default)
#   omnictl media download metal-production --output ./ --format raw # raw disk image (.raw.xz)
#   omnictl media download metal-production --format pxe             # print the PXE boot URL
#   omnictl media download rpi-generic --output ./                   # always a raw disk image
