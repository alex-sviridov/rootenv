# k3s-node

Sandbox environment with one k3s node in Hetzner.

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
| <a name="provider_null"></a> [null](#provider\_null) | 3.3.0 |
| <a name="provider_random"></a> [random](#provider\_random) | 3.9.0 |

## Modules

| Name | Source | Version |
| ---- | ------ | ------- |
| <a name="module_k3s"></a> [k3s](#module\_k3s) | ../../modules/node | n/a |
| <a name="module_service_dns"></a> [service\_dns](#module\_service\_dns) | ../../modules/service-dns | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [null_resource.fetch_kubeconfig](https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource) | resource |
| [random_password.k3s_token](https://registry.terraform.io/providers/hashicorp/random/latest/docs/resources/password) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_allowed_ssh_ips"></a> [allowed\_ssh\_ips](#input\_allowed\_ssh\_ips) | CIDRs allowed to reach SSH and Kubernetes API | `list(string)` | <pre>[<br/>  "0.0.0.0/0"<br/>]</pre> | no |
| <a name="input_cloudflare_api_token"></a> [cloudflare\_api\_token](#input\_cloudflare\_api\_token) | Cloudflare API token, zone-scoped to the infra zone | `string` | n/a | yes |
| <a name="input_dns_zone_id"></a> [dns\_zone\_id](#input\_dns\_zone\_id) | Cloudflare Zone ID | `string` | n/a | yes |
| <a name="input_dns_zone_name"></a> [dns\_zone\_name](#input\_dns\_zone\_name) | DNS zone name for infrastructure records | `string` | n/a | yes |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment name (sandbox/prod) | `string` | `"sandbox"` | no |
| <a name="input_hcloud_token"></a> [hcloud\_token](#input\_hcloud\_token) | Hetzner Cloud API token | `string` | n/a | yes |
| <a name="input_service_dns_name"></a> [service\_dns\_name](#input\_service\_dns\_name) | DNS name for the rootenv service, relative to dns\_zone\_name (e.g. 'sandbox.rootenv' → sandbox.rootenv.example.com) | `string` | `"sandbox"` | no |
| <a name="input_ssh_private_key_path"></a> [ssh\_private\_key\_path](#input\_ssh\_private\_key\_path) | SSH private keys for admin access to the node, used to retrieve kubeconfig | `string` | n/a | yes |
| <a name="input_ssh_public_keys"></a> [ssh\_public\_keys](#input\_ssh\_public\_keys) | SSH public keys for admin access to the node | `list(string)` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_kubeconfig_path"></a> [kubeconfig\_path](#output\_kubeconfig\_path) | Path to the generated kubeconfig |
| <a name="output_next_steps"></a> [next\_steps](#output\_next\_steps) | What to do after apply |
| <a name="output_node_fqdn"></a> [node\_fqdn](#output\_node\_fqdn) | DNS name of the node |
| <a name="output_node_ipv4"></a> [node\_ipv4](#output\_node\_ipv4) | Public IPv4 address of the node |
| <a name="output_service_fqdn"></a> [service\_fqdn](#output\_service\_fqdn) | Public FQDN of the rootenv service |
<!-- END_TF_DOCS -->