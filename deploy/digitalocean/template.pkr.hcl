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

variable "init_flags" {
  type        = string
  description = "Additional swe-swe init flags (beyond --project-directory=/workspace)"
  default     = ""
}

variable "swe_swe_password" {
  type        = string
  description = "Password for swe-swe user (empty for random)"
  default     = ""
  sensitive   = true
}

variable "hardening_level" {
  type        = string
  description = "OS hardening level: none, moderate (default), or comprehensive"
  default     = "moderate"
  validation {
    condition     = contains(["none", "moderate", "comprehensive"], var.hardening_level)
    error_message = "The hardening_level must be 'none', 'moderate', or 'comprehensive'."
  }
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

  # Copy swe-swe binary (must be built locally with `make build` at repo root)
  # For amd64 architecture
  provisioner "file" {
    source      = "${path.root}/../../dist/swe-swe.linux-amd64"
    destination = "/usr/local/bin/swe-swe"
  }

  # Wait for cloud-init to complete before running provisioners
  # Prevents "dpkg lock" errors from concurrent package updates
  provisioner "shell" {
    inline = [
      "echo 'Waiting for cloud-init to complete...'",
      "cloud-init status --wait || true",
      "echo 'Cloud-init completed (or timed out/errored - proceeding anyway)'"
    ]
  }

  # Run installation scripts in order
  provisioner "shell" {
    scripts = concat(
      [
        "scripts/010-docker.sh",
        "scripts/020-swe-swe.sh",
        "scripts/030-systemd.sh",
        "scripts/090-ufw.sh"
      ],
      var.hardening_level == "none" ? [] : ["scripts/011-hardening-moderate.sh"],
      var.hardening_level == "comprehensive" ? ["scripts/012-hardening-comprehensive.sh"] : [],
      [
        "scripts/900-cleanup.sh"
      ]
    )
    environment_vars = [
      "DEBIAN_FRONTEND=noninteractive",
      "SWE_SWE_INIT_FLAGS=${var.init_flags}",
      "SWE_SWE_PASSWORD=${var.swe_swe_password}"
    ]
  }

  post-processor "manifest" {
    output     = "manifest.json"
    strip_path = true
  }
}
