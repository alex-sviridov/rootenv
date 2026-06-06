variable "hcloud_token" {
  description = "Hetzner Cloud API token"
  type        = string
  sensitive   = true
}

variable "ssh_public_keys" {
  description = "SSH public keys for admin access to the node"
  type        = list(string)
}

variable "ssh_private_key_path" {
  description = "SSH private keys for admin access to the node, used to retrieve kubeconfig"
  type        = string
}

variable "allowed_ssh_ips" {
  description = "CIDRs allowed to reach SSH and Kubernetes API"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "environment" {
  description = "Environment name (sandbox/prod)"
  type        = string
  default     = "sandbox"
}

variable "cloudflare_api_token" {
  description = "Cloudflare API token, zone-scoped to the infra zone"
  type        = string
  sensitive   = true
}

variable "dns_zone_id" {
  description = "Cloudflare Zone ID"
  type        = string
}

variable "dns_zone_name" {
  description = "DNS zone name for infrastructure records"
  type        = string
}

variable "service_dns_name" {
  description = "DNS name for the rootenv service, relative to dns_zone_name (e.g. 'sandbox.rootenv' → sandbox.rootenv.example.com)"
  type        = string
  default     = "sandbox"
}