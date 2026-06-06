# Infrastructure

OpenTofu configuration for rootenv. Provisions a single-node k3s cluster on Hetzner Cloud.

## Prerequisites

- [OpenTofu](https://opentofu.org/) >= 1.6
- `kubectl`
- Hetzner Cloud API token
- SSH key at `~/.ssh/id_ed25519`

## Quickstart

```bash
cd environments/sandbox

cp terraform.tfvars.example terraform.tfvars   # fill in values

tofu init
tofu apply

export KUBECONFIG=~/.kube/rootenv-sandbox
kubectl get nodes
```

## Tear down

```bash
cd environments/sandbox
tofu destroy
rm ~/.kube/rootenv-sandbox
```

## Layout

- `modules/node/` — Hetzner VM + cloud-init bootstrap with k3s
- `environments/sandbox/` — disposable test environment

State is stored locally. Migration to Hetzner Object Storage is planned;
see commented `backend.tf` in each environment.

## Further reading

For architecture overview and design decisions, see [docs/infra.md](../../docs/infra.md).