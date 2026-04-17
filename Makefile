dev:
	docker compose -f infra/compose-dev.yaml watch

dev-build:
	docker compose -f infra/compose-dev.yaml build --no-cache