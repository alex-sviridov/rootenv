# k3s-node

Sandbox environment with one k3s node in Hetzner.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.2 |
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

## Resources

| Name | Type |
| ---- | ---- |
| [null_resource.fetch_kubeconfig](https://registry.terraform.io/providers/hashicorp/null/latest/docs/resources/resource) | resource |
| [random_password.k3s_token](https://registry.terraform.io/providers/hashicorp/random/latest/docs/resources/password) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_allowed_ssh_ips"></a> [allowed\_ssh\_ips](#input\_allowed\_ssh\_ips) | CIDRs allowed to reach SSH and Kubernetes API | `list(string)` | <pre>[<br/>  "0.0.0.0/0"<br/>]</pre> | no |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment name (sandbox/prod) | `string` | `"sandbox"` | no |
| <a name="input_hcloud_token"></a> [hcloud\_token](#input\_hcloud\_token) | Hetzner Cloud API token | `string` | n/a | yes |
| <a name="input_ssh_public_keys"></a> [ssh\_public\_keys](#input\_ssh\_public\_keys) | SSH public keys for admin access to the node | `list(string)` | n/a | yes |

## Outputs

No outputs.
<!-- END_TF_DOCS -->