
.PHONY: all
all: build

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
vet: fmt vet ## Run go test against code.
	go test ./...

.PHONY: build
build:  fmt vet ## Build a binary.
	go build -o bin/controller cmd/main.go

.PHONY: run
run:  fmt vet ## Run a controller from your host.
	export $$(cat .env | xargs) && go run ./cmd/main.go
