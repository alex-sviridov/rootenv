.PHONY: test lint audit scan ci-dryrun

test:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/.npm:/root/.npm \
		node:22-alpine sh -c "npm ci && npm run test:unit -- --run"

lint:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/.npm:/root/.npm \
		node:22-alpine sh -c "npm ci && npm run lint"

audit:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/.npm:/root/.npm \
		node:22-alpine sh -c "npm ci && npm audit --audit-level=high"

scan:
	docker run --rm \
		-v $(PWD):/app \
		-v trivy-cache:/root/.cache/trivy \
		aquasec/trivy fs \
		--scanners vuln,secret,misconfig \
		--ignore-unfixed \
		/app

ci-dryrun: test lint audit scan
