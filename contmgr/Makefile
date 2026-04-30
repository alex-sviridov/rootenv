.PHONY: test lint scan govulncheck ci-dryrun

test:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/gopath/pkg/mod:/go/pkg/mod \
		golang:1.26 go test ./...

lint:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		golangci/golangci-lint golangci-lint run ./...

scan:
	docker run --rm \
		-v $(PWD):/app \
		-v trivy-cache:/root/.cache/trivy \
		aquasec/trivy fs \
		--scanners vuln,secret,misconfig \
		--ignore-unfixed \
		/app

govulncheck:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/gopath/pkg/mod:/go/pkg/mod \
		golang:1.26 sh -c "go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./..."

ci-dryrun: test lint scan govulncheck
