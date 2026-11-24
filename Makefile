# Common development tasks. Every target also works on Windows with plain
# "make" (cmd.exe): the Unix-only steps have a Windows variant below.

ifeq ($(OS),Windows_NT)
VERSION ?= dev
else
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
endif
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

race: ## Run unit tests with the race detector (needs cgo and a C compiler such as gcc)
	go test -race ./...

integration: ## Run tests against a real Docker daemon
	go test -tags integration -count=1 ./tests/...

vet:
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

ifeq ($(OS),Windows_NT)
fmt-check: ## List files that are not gofmt-formatted (CI enforces this on Linux)
	gofmt -l .

cover: ## Print test coverage per package
	go test -cover ./...

clean:
	-@del /Q /F $(BIN) coverage.out 2>NUL
else
fmt-check: ## Fail if any file is not gofmt-formatted
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

cover: ## Print test coverage per package
	go test -cover ./...

clean:
	rm -f $(BIN) coverage.out
endif

docker: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t containerdrift:$(VERSION) .