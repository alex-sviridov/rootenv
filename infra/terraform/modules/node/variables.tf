variable "name" {
  description = "Server name and base for resource labels"
  type        = string
}

variable "dns_zone_name" {
  description = "DNS zone name, used to build FQDN (e.g. example.com)"
  type        = string
}

variable "dns_name" {
  description = "DNS record name relative to dns_zone_name (e.g. 'node-01.srv' becomes 'node-01.srv.example.com')"
  type        = string
}

variable "environment" {
  description = "Environment name (sandbox/prod)"
  type        = string
}

variable "server_type" {
  description = "Hetzner server type"
  type        = string
  default     = "cx23"
}

variable "location" {
  description = "Hetzner datacenter location"
  type        = string
  default     = "nbg1"
}

variable "image" {
  description = "Base OS image"
  type        = string
  default     = "rocky-10"
}

variable "ssh_public_keys" {
  description = "SSH public keys for admin access to the node"
  type        = list(string)
}

variable "k3s_version" {
  description = "Pinned k3s version (immutable infra principle)"
  type        = string
  default     = "v1.36.1+k3s1"
}

variable "allowed_ssh_ips" {
  description = "CIDRs allowed to reach SSH. Restrict to your IP."
  type        = list(string)
  default = [
    "0.0.0.0/0",
    "::/0"
  ]
}

variable "k3s_token" {
  description = "Shared secret for k3s cluster join. Used by future nodes to join the cluster."
  type        = string
  sensitive   = true
}

variable "dns_zone_id" {
  description = "Cloudflare Zone ID for the infrastructure DNS zone (e.g. srv.example.com)"
  type        = string
}
