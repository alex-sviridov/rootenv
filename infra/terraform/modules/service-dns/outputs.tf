output "fqdn" {
  description = "Fully qualified domain name of the service"
  value       = "${var.dns_name}.${var.dns_zone_name}"
}