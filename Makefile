dev-cluster:
	k3d cluster delete rootenv || true
	k3d cluster create --config deploy/k3d.yaml
	kubectl apply -f deploy/k8s/00-namespace-infra.yaml
	kubectl apply -f deploy/config/*.dev.yaml
	kubectl wait --for=create crd/middlewares.traefik.io --timeout=90s
	kubectl apply -f deploy/k8s/

dev:
	skaffold dev --cleanup=false

prod-deploy:
	kubectl apply -f deploy/k8s/00-namespace-infra.yaml
	kubectl apply -f deploy/config/contmgr.prod.yaml
	kubectl apply -f deploy/k8s/

labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/

labs-build:
	docker build -t rootenv-ubuntu-sshd:latest labs/images/ubuntu-sshd
	k3d image import rootenv-ubuntu-sshd:latest -c rootenv