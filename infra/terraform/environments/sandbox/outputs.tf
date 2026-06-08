output "node_fqdn" {
  description = "DNS name of the node"
  value       = module.k3s.fqdn
}

output "node_ipv4" {
  description = "Public IPv4 address of the node"
  value       = module.k3s.ipv4_address
}

output "kubeconfig_path" {
  description = "Path to the generated kubeconfig"
  value       = abspath("${path.module}/kubeconfig")
}

output "service_fqdn" {
  description = "Public FQDN of the rootenv service"
  value       = module.service_dns.fqdn
}

output "next_steps" {
  description = "What to do after apply"
  value       = <<-EOT
    
    Environment ready.
    
    Node:       ${module.k3s.fqdn} (${module.k3s.ipv4_address})
    Kubeconfig: ${local.kubeconfig_path}
    
    Quick start:
      export KUBECONFIG=${local.kubeconfig_path}
      kubectl get nodes
    
    Break-glass SSH:
      ssh admin@${module.k3s.fqdn}
  EOT
}