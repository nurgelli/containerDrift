# Common development tasks. On Windows, run these from Git Bash or WSL, or
# copy the underlying "go" commands.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

ifeq ($(OS),Windows_NT)
BIN := containerdrift.exe
else
BIN := containerdrift
endif

.PHONY: build test race integration vet fmt fmt-check cover clean docker

build: ## Build the binary into the current directory
	go build $(LDFLAGS) -o $(BIN) ./cmd/containerdrift

test: ## Run unit tests (no Docker needed)
	go test ./...

race: ## Run unit tests with the race detector (needs a C compiler)
	go test -race ./...

integration: ## Run tests against a real Docker daemon
	go test -tags integration -count=1 ./tests/...

vet:
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

fmt-check: ## Fail if any file is not gofmt-formatted
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

cover: ## Write coverage.out and print the total
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t containerdrift:$(VERSION) .

clean:
	rm -f $(BIN) coverage.out
