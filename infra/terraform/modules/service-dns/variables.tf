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
  description = "IPv4 address the record points to."
  type        = string
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