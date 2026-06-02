# Infrastructure

The rootenv platform k3s cluster is provisioned using terraform\opentofu which process is described in this document.

## Overview

Here we provision single VM in Hetzner with k3s installed. As we are in MVP state, single-node infra is used. Kubectl is automatically retrieved using SSH.

## Architecture Decisions

- Why OpenTofu not Terraform?
- Why k3s (not k8s/k0s/microk8s)?
- Why cloud-init (not Ansible)?
- Why Hetzner not <any other provider>? 
- Why AWS for state?


## Cost

Sandbox environment: ~€5/month (as for 02/06/26)
- CX22 server - €4.67 / mo
- IPv4 address - €0.59 / mo

## Getting started

To provision an environment, follow the [infra README](../infra/terraform/README.md).