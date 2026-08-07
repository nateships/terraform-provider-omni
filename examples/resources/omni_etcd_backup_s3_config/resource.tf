variable "s3_access_key_id" {
  type      = string
  sensitive = true
}

variable "s3_secret_access_key" {
  type      = string
  sensitive = true
}

# Instance-wide S3 storage for etcd backups.
resource "omni_etcd_backup_s3_config" "backups" {
  bucket   = "omni-etcd-backups"
  region   = "us-east-1"
  endpoint = "https://s3.example.com"

  access_key_id     = var.s3_access_key_id
  secret_access_key = var.s3_secret_access_key
}

# Backups are only taken for clusters that schedule them.
resource "omni_cluster" "example" {
  name               = "example"
  kubernetes_version = "1.36.2"
  talos_version      = "1.13.5"

  backup_interval = "1h"

  depends_on = [omni_etcd_backup_s3_config.backups]
}
