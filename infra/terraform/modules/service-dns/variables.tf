variable "dns_zone_id" {
  description = "Cloudflare Zone ID for the infrastructure DNS zone (e.g. srv.example.com)"
  type        = string
}

variable "dns_zone_name" {
  description = "DNS zone name, used to build FQDN (e.g. example.com)"
  type        = string
}

variable "dns_name" {
  description = "DNS record name relative to dns_zone_name (e.g. 'service.dev' becomes 'service.dev.example.com')"
  type        = string
}

variable "target_ipv4" {
  description = "List of IPv4 addresses the record points to. Use one IP for single-node, multiple for round-robin, or one LB IP for HA."
  type        = list(string)
  validation {
    condition     = length(var.target_ipv4) > 0
    error_message = "At least one target IP must be provided."
  }
}

variable "proxied" {
  description = "Whether to proxy traffic through Cloudflare (DDoS protection, TLS, hides origin IP)"
  type        = bool
  default     = true
}

variable "ttl" {
  description = "DNS TTL in seconds. Cloudflare requires 1 (=auto) when proxied=true."
  type        = number
  default     = 1
}

variable "environment" {
  description = "Environment name, used in record comment"
  type        = string
}