.PHONY: dev-cluster dev prod-deploy dbusers-init labs-sync labs-build

dev-cluster:
	k3d cluster delete rootenv || true
	k3d cluster create --config deploy/k3d.yaml
	k3d kubeconfig merge rootenv --kubeconfig-merge-default --kubeconfig-switch-context
	kubectl apply -f deploy/base/00-namespace-infra.yaml
	kubectl wait --for=create crd/middlewares.traefik.io --timeout=90s
	kubectl apply -k deploy/overlays/dev/

dev:
	skaffold dev --cleanup=false --trigger=polling

sandbox-platform-deploy:
	/bin/bash deploy/platform/install.sh

sandbox-deploy:
	kubectl apply -k deploy/overlays/sandbox/

dbusers-init:
	python3 ./scripts/dbusers-init.py

labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/

labs-build:
	docker build -t ubuntu-sshd:latest labs/images/ubuntu-sshd
	k3d image import ubuntu-sshd:latest -c rootenv

pre-commit:
	pre-commit run --all-files