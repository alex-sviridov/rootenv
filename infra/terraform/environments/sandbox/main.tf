resource "random_password" "k3s_token" {
  length  = 64
  special = false # k3s token is alphanumeric

  # Lifecycle to protector from non-intended recreation
  lifecycle {
    ignore_changes = [length, special]
  }
}

locals {
  kubeconfig_path = pathexpand("~/.kube/rootenv-${var.environment}")
}

module "k3s" {
  source          = "../../modules/node"
  name            = "node1"
  dns_name            = "node1.infra"
  environment     = var.environment
  ssh_public_keys = var.ssh_public_keys
  allowed_ssh_ips = var.allowed_ssh_ips
  k3s_token       = random_password.k3s_token.result
  dns_zone_id   = var.dns_zone_id
  dns_zone_name = var.dns_zone_name
}

# Fetch kubeconfig; Cannot do it without SSH
resource "null_resource" "fetch_kubeconfig" {
  depends_on = [module.k3s]

  triggers = {
    node_id = module.k3s.fqdn
  }

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      set -euo pipefail

      SSH_OPTS="-i ${var.ssh_private_key_path} -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o LogLevel=ERROR"
      HOST="admin@${module.k3s.fqdn}"

      echo "waiting for k3s bootstrap to complete..."
      until ssh $SSH_OPTS "$HOST" 'test -f /var/lib/rootenv-bootstrap-complete' 2>/dev/null; do
        sleep 5
      done

      echo "fetching kubeconfig..."
      mkdir -p "$HOME/.kube"
      ssh $SSH_OPTS "$HOST" 'sudo cat /etc/rancher/k3s/k3s.yaml' > "${local.kubeconfig_path}"
      chmod 600 "${local.kubeconfig_path}"

      sed -i "s|127.0.0.1|${module.k3s.ipv4_address}|" "${local.kubeconfig_path}"

      kubectl --kubeconfig="${local.kubeconfig_path}" config rename-context default rootenv-${var.environment} 2>/dev/null || true
    EOT
  }
}

module "service_dns" {
  source = "../../modules/service-dns"

  dns_zone_id     = var.dns_zone_id
  dns_zone_name   = var.dns_zone_name
  dns_name        = var.service_dns_name         
  target_ipv4 = [module.k3s.ipv4_address]    
  proxied     = true
  environment = var.environment
}