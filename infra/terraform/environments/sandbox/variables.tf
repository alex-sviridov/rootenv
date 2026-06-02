variable "hcloud_token" {
  description = "Hetzner Cloud API token"
  type        = string
  sensitive   = true
}

variable "ssh_public_keys" {
  description = "SSH public keys for admin access to the node"
  type        = list(string)
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