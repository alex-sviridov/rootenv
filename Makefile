
skaffold-run:
	skaffold run --cleanup=false

k3d-run:
	k3d cluster start rootenv

dev: k3d-run
	skaffold dev --cleanup=false

# Recreate the k3d cluster, fix the containerd registry config, push all images,
# and apply k8s manifests. Run once after a cluster recreate.
cluster:
	k3d cluster delete rootenv || true
	k3d cluster create --config k3d.yaml
	k3d kubeconfig merge rootenv --kubeconfig-merge-default
	$(MAKE) fix-registry
	skaffold build
	$(MAKE) push-latest
	kubectl apply -f deploy/k8s/

# Patch the hosts.toml that k3s auto-generates with https — must use http for the local registry.
# Re-run this if pods get "http: server gave HTTP response to HTTPS client".
fix-registry:
	docker exec k3d-rootenv-server-0 sh -c '\
		mkdir -p "/var/lib/rancher/k3s/agent/etc/containerd/certs.d/rootenv-registry:5000" && \
		printf "[server]\nserver = \"http://rootenv-registry:5000\"\n\n[host.\"http://rootenv-registry:5000\"]\n  capabilities = [\"pull\", \"resolve\", \"push\"]\n  skip_verify = true\n" \
		> "/var/lib/rancher/k3s/agent/etc/containerd/certs.d/rootenv-registry:5000/hosts.toml"'

# Tag the current commit-SHA images as :latest so pods that request :latest can pull them.
# Skaffold tags by git SHA; lab YAMLs use bare image names which default to :latest.
push-latest:
	@for img in rootenv-backend rootenv-relay-ssh rootenv-contmgr rootenv-ubuntu-sshd rootenv-frontend; do \
		tag=$$(curl -s http://localhost:5111/v2/$$img/tags/list | python3 -c "import sys,json; tags=[t for t in json.load(sys.stdin).get('tags',[]) if t!='latest']; print(tags[-1] if tags else '')" 2>/dev/null); \
		if [ -n "$$tag" ]; then \
			echo "Tagging $$img:$$tag as latest"; \
			docker tag localhost:5111/$$img:$$tag localhost:5111/$$img:latest; \
			docker push localhost:5111/$$img:latest; \
		fi; \
	done

labs-sync:
	python3 ./scripts/labs-sync.py labs/definitions/