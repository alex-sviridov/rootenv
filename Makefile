dev:
	docker compose -f infra/compose-dev.yaml watch

dev-build:
	docker compose -f infra/compose-dev.yaml build --no-cache

labs-sync:
	python3 labs/sync.py

test:
	cd frontend && npm run test:unit 

test-e2e:
	cd frontend && npm run test:e2e 