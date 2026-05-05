TODO!
## Deployment Topologies

### Local Development
For local development and testing, use k3d:
\`\`\`
make dev-cluster
skaffold dev
\`\`\`

### Single-Node Production (k3s)
For demo and small-scale production deployments:
[steps to install k3s, apply manifests]

### Multi-Node Production
For production HA setup, the platform deploys to any conformant Kubernetes cluster.
Tested with: k3s HA, EKS, vanilla kubeadm clusters.
[notes about what configuration changes for HA]