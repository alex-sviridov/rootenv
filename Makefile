
skaffold-run:
	skaffold run --cleanup=false

k3d-run:
	k3d cluster start rootenv

dev: k3d-run
	skaffold dev --cleanup=false

# Recreate the k3d cluster, deploy the in-cluster registry, push all images,
# and apply remaining k8s manifests. Run once after a cluster recreate.
cluster:
	k3d cluster delete rootenv || true
	k3d cluster create --config deploy/k3d.yaml
	k3d kubeconfig merge rootenv --kubeconfig-merge-default
	kubectl apply -f deploy/k8s/00-namespace-infra.yaml
	kubectl apply -f deploy/k8s/01-registry.yaml
	kubectl rollout status deployment/registry -n rootenv-infra --timeout=120s
	skaffold build
	$(MAKE) push-latest
	kubectl apply -f deploy/k8s/

# Tag all images at the current git SHA as :latest in the registry.
# Skaffold tags by git SHA; lab YAMLs reference bare image names which resolve to :latest.
push-latest:
	$(eval SHA := $(shell git rev-parse --short HEAD))
	@for img in rootenv-backend rootenv-relay-ssh rootenv-contmgr rootenv-ubuntu-sshd rootenv-frontend; do \
		echo "Tagging $$img:$(SHA) as latest"; \
		docker tag localhost:5111/$$img:$(SHA) localhost:5111/$$img:latest; \
		docker push localhost:5111/$$img:latest; \
	done

dbusers-init:
	python3 scripts/dbusers-init.py

labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/

list-images:
	@curl -s http://localhost:5111/v2/_catalog | python3 -c "import sys,json; print('\n'.join(json.load(sys.stdin).get('repositories',[])))"