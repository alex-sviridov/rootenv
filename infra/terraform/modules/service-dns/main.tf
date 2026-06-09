resource "cloudflare_record" "this" {
  zone_id = var.dns_zone_id
  name    = var.dns_name
  type    = "A"
  content = var.target_ipv4
  ttl     = var.proxied ? 1 : var.ttl
  proxied = var.proxied
  comment = "Managed by rootenv/infra/terraform — service ${var.dns_name} — ${var.environment}"
}