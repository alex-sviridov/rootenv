resource "random_password" "k3s_token" {
  length  = 64
  special = false # k3s token is alphanumeric

  # Lifecycle to protector from non-intended recreation
  lifecycle {
    ignore_changes = [length, special]
  }
}

module "k3s" {
  source          = "../../modules/node"
  name            = "node1"
  environment     = var.environment
  ssh_public_keys = var.ssh_public_keys
  allowed_ssh_ips = var.allowed_ssh_ips
  k3s_token       = random_password.k3s_token.result
}

# Fetch kubeconfig; Cannot do it without SSH
resource "null_resource" "fetch_kubeconfig" {
  depends_on = [module.k3s]

  triggers = {
    node_id = module.k3s.ipv4_address
  }

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      set -euo pipefail

      SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o LogLevel=ERROR"
      HOST="admin@${module.k3s.ipv4_address}"
      DEST="$HOME/.kube/rootenv-${var.environment}"

      echo "waiting for k3s bootstrap to complete..."
      until ssh $SSH_OPTS "$HOST" 'test -f /var/lib/rootenv-bootstrap-complete' 2>/dev/null; do
        sleep 5
      done

      echo "fetching kubeconfig..."
      mkdir -p "$HOME/.kube"
      ssh $SSH_OPTS "$HOST" 'sudo cat /etc/rancher/k3s/k3s.yaml' > "$DEST"
      chmod 600 "$DEST"

      sed -i "s|127.0.0.1|${module.k3s.ipv4_address}|" "$DEST"

      kubectl --kubeconfig="$DEST" config rename-context default rootenv-${var.environment} 2>/dev/null || true

      echo ""
      echo "kubeconfig installed at $DEST"
      echo "use: export KUBECONFIG=$DEST"
    EOT
  }
}