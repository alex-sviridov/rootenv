output "ipv4_address" {
  description = "Public IPv4 of the k3s node"
  value       = hcloud_server.node.ipv4_address
}

output "name" {
  value = hcloud_server.node.name
}