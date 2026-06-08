## Install platform components

Run once after cluster creation:

\`\`\`bash
helm repo add traefik https://traefik.github.io/charts
helm repo update
helm install traefik traefik/traefik \
  --namespace traefik-system --create-namespace
\`\`\`

Ingresses route through Traefik's `websecure` entrypoint (port 443),
which is enabled by default and serves Traefik's built-in self-signed
certificate. Cloudflare's zone SSL/TLS mode is set to "Full" (see the
`service-dns` Terraform module) so it connects to the origin over
HTTPS without validating that certificate against a CA.