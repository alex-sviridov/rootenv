.PHONY: test lint scan govulncheck ci-dryrun

test:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
		-v $(HOME)/gopath/pkg/mod:/go/pkg/mod \
<<<<<<< HEAD
		golang:1.26 go test ./...
=======
		golang:1.26 go test -v ./... && go build -v ./...
>>>>>>> 2899783 (ci: contmgr)

lint:
	docker run --rm \
		-v $(PWD):/app \
		-w /app \
<<<<<<< HEAD
		golangci/golangci-lint golangci-lint run ./...
=======
		-v $(HOME)/gopath/pkg/mod:/go/pkg/mod \
		-v $(HOME)/gopath/bin:/go/bin \
		-v go-build-cache:/root/.cache/go-build \
		golang:1.26 sh -c "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && golangci-lint run ./..."
>>>>>>> 2899783 (ci: contmgr)

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
<<<<<<< HEAD
=======
		-v $(HOME)/gopath/bin:/go/bin \
		-v go-build-cache:/root/.cache/go-build \
>>>>>>> 2899783 (ci: contmgr)
		golang:1.26 sh -c "go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./..."

ci-dryrun: test lint scan govulncheck
