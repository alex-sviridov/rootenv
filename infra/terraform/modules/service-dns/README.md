# node

Cloudflare DNS record.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.6.0 |
| <a name="requirement_cloudflare"></a> [cloudflare](#requirement\_cloudflare) | ~> 4.40 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_cloudflare"></a> [cloudflare](#provider\_cloudflare) | ~> 4.40 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [cloudflare_record.this](https://registry.terraform.io/providers/cloudflare/cloudflare/latest/docs/resources/record) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_dns_name"></a> [dns\_name](#input\_dns\_name) | DNS record name relative to dns\_zone\_name (e.g. 'service.dev' becomes 'service.dev.example.com') | `string` | n/a | yes |
| <a name="input_dns_zone_id"></a> [dns\_zone\_id](#input\_dns\_zone\_id) | Cloudflare Zone ID for the infrastructure DNS zone (e.g. srv.example.com) | `string` | n/a | yes |
| <a name="input_dns_zone_name"></a> [dns\_zone\_name](#input\_dns\_zone\_name) | DNS zone name, used to build FQDN (e.g. example.com) | `string` | n/a | yes |
| <a name="input_environment"></a> [environment](#input\_environment) | Environment name, used in record comment | `string` | n/a | yes |
| <a name="input_proxied"></a> [proxied](#input\_proxied) | Whether to proxy traffic through Cloudflare (DDoS protection, TLS, hides origin IP) | `bool` | `true` | no |
| <a name="input_target_ipv4"></a> [target\_ipv4](#input\_target\_ipv4) | IPv4 address the record points to. | `string` | n/a | yes |
| <a name="input_ttl"></a> [ttl](#input\_ttl) | DNS TTL in seconds. Cloudflare requires 1 (=auto) when proxied=true. | `number` | `1` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_fqdn"></a> [fqdn](#output\_fqdn) | Fully qualified domain name of the service |
<!-- END_TF_DOCS -->