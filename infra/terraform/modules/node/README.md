# node

Hetzner Cloud VM with k3s installed via cloud-init.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.2 |
| <a name="requirement_cloudflare"></a> [cloudflare](#requirement\_cloudflare) | ~> 4.40 |
| <a name="requirement_hcloud"></a> [hcloud](#requirement\_hcloud) | ~> 1.45 |
| <a name="requirement_random"></a> [random](#requirement\_random) | ~> 3.6 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_cloudflare"></a> [cloudflare](#provider\_cloudflare) | ~> 4.40 |
| <a name="provider_hcloud"></a> [hcloud](#provider\_hcloud) | ~> 1.45 |
| <a name="provider_null"></a> [null](#provider\_null) | n/a |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [cloudflare_record.node_a](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/record) | resource |
| [cloudflare_record.node_aaaa](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/record) | resource |
| [hcloud_firewall.node](https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs/resources/firewall) | resource |
| [hcloud_primary_ip.node_ipv4](https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs/resources/primary_ip) | resource |
| [hcloud_server.node](https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs/resources/server) | resource |
| [hcloud_ssh_key.admin](https://registry.terraform.io/providers/hetznercloud/hcloud/latest/docs/resources/ssh_key) | resource |
| [null_resource.prune_known_hosts](https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_allowed_ssh_ips"></a> [allowed\_ssh\_ips](#input\_allowed\_ssh\_ips) | CIDRs allowed to reach SSH. Restrict to your IP. | `list(string)` | <pre>[<br/>  "0.0.0.0/0",<br/>  "::/0"<br/>]</pre> | no |
| <a name="input_dns_name"></a> [dns\_name](#input\_dns\_name) | DNS record name relative to dns\_zone\_name (e.g. 'node-01.srv' becomes 'node-01.srv.example.com') | `string` | n/a | yes |
| <a name="input_dns_zone_id"></a> [dns\_zone\_id](#input\_dns\_zone\_id) | Cloudflare Zone ID for the infrastructure DNS zone (e.g. srv.example.com) | `string` | n/a | yes |
| <a name="input_dns_zone_name"></a> [dns\_zone\_name](#input\_dns\_zone\_name) | DNS zone name, used to build FQDN (e.g. example.com) | `string` | n/a | yes |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment name (sandbox/prod) | `string` | n/a | yes |
| <a name="input_image"></a> [image](#input\_image) | Base OS image | `string` | `"rocky-10"` | no |
| <a name="input_k3s_token"></a> [k3s\_token](#input\_k3s\_token) | Shared secret for k3s cluster join. Used by future nodes to join the cluster. | `string` | n/a | yes |
| <a name="input_k3s_version"></a> [k3s\_version](#input\_k3s\_version) | Pinned k3s version (immutable infra principle) | `string` | `"v1.36.1+k3s1"` | no |
| <a name="input_location"></a> [location](#input\_location) | Hetzner datacenter location | `string` | `"nbg1"` | no |
| <a name="input_name"></a> [name](#input\_name) | Server name and base for resource labels | `string` | n/a | yes |
| <a name="input_server_type"></a> [server\_type](#input\_server\_type) | Hetzner server type | `string` | `"cx23"` | no |
| <a name="input_ssh_public_keys"></a> [ssh\_public\_keys](#input\_ssh\_public\_keys) | SSH public keys for admin access to the node | `list(string)` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_fqdn"></a> [fqdn](#output\_fqdn) | Fully qualified domain name of the node |
| <a name="output_ipv4_address"></a> [ipv4\_address](#output\_ipv4\_address) | Public IPv4 of the k3s node |
| <a name="output_name"></a> [name](#output\_name) | n/a |
<!-- END_TF_DOCS -->