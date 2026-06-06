terraform {
  required_version = ">= 1.2"

  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.45"
    }

    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }


    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 4.40"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}


provider "cloudflare" {
  api_token = var.cloudflare_api_token
}