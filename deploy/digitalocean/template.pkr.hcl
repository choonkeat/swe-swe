packer {
  required_plugins {
    digitalocean = {
      version = ">= 1.0.0"
      source  = "github.com/digitalocean/digitalocean"
    }
  }
}

variable "do_token" {
  type        = string
  description = "DigitalOcean API token"
  default     = env("DIGITALOCEAN_API_TOKEN")
  sensitive   = true
}

variable "image_name" {
  type        = string
  description = "Name for the snapshot"
  default     = "swe-swe"
}

variable "image_version" {
  type        = string
  description = "Version tag for the snapshot (required)"
}

variable "droplet_size" {
  type        = string
  description = "Droplet size for building (see https://docs.digitalocean.com/reference/api/list-regions/)"
  default     = "s-2vcpu-4gb"
}

variable "region" {
  type        = string
  description = "DigitalOcean region (required; see https://docs.digitalocean.com/reference/api/list-regions/)"
}

locals {
  timestamp    = formatdate("YYYYMMDD-hhmmss", timestamp())
  snapshot_name = "${var.image_name}-${var.image_version}-${local.timestamp}"
}

source "digitalocean" "swe-swe" {
  api_token     = var.do_token
  image         = "ubuntu-24-04-x64"
  region        = var.region
  size          = var.droplet_size
  ssh_username  = "root"
  snapshot_name = local.snapshot_name

  snapshot_tags = [
    "swe-swe",
    "marketplace"
  ]
}

build {
  sources = ["source.digitalocean.swe-swe"]

  # Copy files to be provisioned
  provisioner "file" {
    source      = "files/"
    destination = "/"
  }

  # Run installation scripts in order
  provisioner "shell" {
    scripts = [
      "scripts/010-docker.sh",
      "scripts/020-swe-swe.sh",
      "scripts/030-systemd.sh",
      "scripts/090-ufw.sh",
      "scripts/900-cleanup.sh"
    ]
    environment_vars = [
      "DEBIAN_FRONTEND=noninteractive"
    ]
  }

  post-processor "manifest" {
    output     = "manifest.json"
    strip_path = true
  }
}
