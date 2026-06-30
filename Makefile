ifneq (,$(wildcard ./.env))
    include .env
    export
endif

.PHONY: dev-cluster dev prod-deploy dbusers-init labs-sync

sandbox sandbox-platform-deploy sandbox-deploy: export SKAFFOLD_KUBECONFIG = $(SANDBOX_KUBECONFIG)
sandbox sandbox-platform-deploy sandbox-deploy: export SKAFFOLD_DEFAULT_REPO = $(SANDBOX_REPO)
.dev-dbusers-init .dev-labs-sync: export POCKETBASE_URL = http://localhost:80/api/


.dev-cluster-remove:
	k3d cluster delete rootenv || true

.dev-cluster-create:
	k3d cluster create --config deploy/k3d.yaml
	k3d kubeconfig merge rootenv --kubeconfig-merge-default --kubeconfig-switch-context
	kubectl wait --for=condition=Ready node --all --timeout=90s
	kubectl apply -f deploy/base/00-namespace-infra.yaml
	kubectl wait --for=create crd/middlewares.traefik.io --timeout=90s
	k3d kubeconfig get rootenv > ~/.kube/rootenv-dev

dev-rebuild:
	skaffold run --cache-artifacts=false

.wait-backend:
	kubectl wait --for=condition=Available deployment/backend -n rootenv-infra --timeout=90s

dev-cluster: .dev-cluster-remove .dev-cluster-create dev-rebuild .wait-backend .dev-dbusers-init .dev-labs-sync

.dev-dbusers-init:
	python3 ./scripts/dbusers-init.py

.dev-labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/

dev-polling:
	skaffold dev --cleanup=false --trigger=polling

sandbox:
	skaffold run -p sandbox --cache-artifacts=false

sandbox-platform-deploy:
	/bin/bash deploy/platform/install.sh

sandbox-deploy:
	kubectl apply -k deploy/overlays/sandbox/

dbusers-init:
	python3 ./scripts/dbusers-init.py

labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/

pre-commit:
	pre-commit run --all-files