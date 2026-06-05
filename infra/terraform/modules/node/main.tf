locals {
  node_fqdn = "${var.dns_name}.${var.dns_zone_name}"
}

resource "hcloud_ssh_key" "admin" {
  count      = length(var.ssh_public_keys)
  name       = "${var.name}-admin-${count.index}"
  public_key = var.ssh_public_keys[count.index]
}

resource "hcloud_firewall" "node" {
  name = "${var.name}-fw"
  rule {
    direction  = "in"
    protocol   = "icmp"
    source_ips = var.allowed_ssh_ips
  }

  # SSH
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = var.allowed_ssh_ips
  }

  # K3S
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "6443"
    source_ips = var.allowed_ssh_ips
  }

  # USER ACCESS ALLOWED
  # TODO: Limit to Cloudflare hosts #33
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "80"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }

}

resource "hcloud_primary_ip" "node_ipv4" {
  name        = "${var.name}-ipv4"
  location    = var.location
  type        = "ipv4"
  auto_delete = false

  labels = {
    project = "rootenv"
    env     = var.environment
  }
}

resource "hcloud_server" "node" {
  name        = var.name
  server_type = var.server_type
  image       = var.image
  location    = var.location
  ssh_keys    = hcloud_ssh_key.admin[*].id

  firewall_ids = [hcloud_firewall.node.id]

  public_net {
    ipv4_enabled = true
    ipv4         = hcloud_primary_ip.node_ipv4.id
    ipv6_enabled = true
  }

  user_data = templatefile("${path.module}/cloud-init.yaml.tftpl", {
    node_name       = var.name
    environment     = var.environment
    k3s_version     = var.k3s_version
    k3s_token       = var.k3s_token
    node_fqdn   = local.node_fqdn
    ssh_public_keys = var.ssh_public_keys
  })

  labels = {
    project = "rootenv"
    env     = var.environment
    role    = "k3s-server"
  }
}

resource "cloudflare_record" "node_a" {
  zone_id = var.dns_zone_id
  name    = var.dns_name
  type    = "A"
  content = hcloud_server.node.ipv4_address
  ttl     = 3600
  proxied = false
  comment = "Managed by rootenv/infra/terraform — ${var.environment}"
}

resource "cloudflare_record" "node_aaaa" {
  zone_id = var.dns_zone_id
  name    = var.dns_name
  type    = "AAAA"
  content = hcloud_server.node.ipv6_address
  ttl     = 3600
  proxied = false
  comment = "Managed by rootenv/infra/terraform — ${var.environment}"
}
