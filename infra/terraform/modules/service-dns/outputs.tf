output "fqdn" {
  description = "Fully qualified domain name of the service"
  value       = "${var.dns_name}.${var.dns_zone_name}"
}

output "record_ids" {
  description = "IDs of created DNS records (one per target IP)"
  value       = { for ip, r in cloudflare_record.this : ip => r.id }
}