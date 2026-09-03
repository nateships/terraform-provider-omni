# A machine class provisioned on demand by an infrastructure provider.
resource "omni_machine_class" "proxmox_worker" {
  name = "proxmox-worker"

  auto_provision = {
    provider_id = "proxmox"
    kernel_args = ["console=ttyS0"]

    provider_data = yamlencode({
      cores     = 8
      memory    = 16384
      disk_size = 100
    })

    # Talos META partition entries for the provisioned machines.
    meta_values = [
      { key = 10, value = "worker" },
    ]
  }
}

# A machine class matching existing machines by labels.
resource "omni_machine_class" "manual_pool" {
  name         = "manual-pool"
  match_labels = ["amd64"]
}
