migr-sync:
	./backend/pull-migrations.sh

dev:
	skaffold dev --cleanup=false

labs-sync:
	python3 labs/sync.py

test:
	cd frontend && npm run test:unit 

test-e2e:
	cd frontend && npm run test:e2e 