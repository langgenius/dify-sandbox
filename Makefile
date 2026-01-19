.PHONY: help run test lint lint-fix fmt vet install-deps clean build pre-commit-install

# Variables
BINARY_NAME=main
GO=go
GOFLAGS=-v
DOCKER_REGISTRY=ghcr.io/agent-infra/sandbox
SANDBOX_PORT=10000

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

run: ## Run the server
	@echo "Starting server..."
	@$(GO) run cmd/server/main.go

build: ## Build the server binary
	@echo "Building $(BINARY_NAME)..."
	@$(GO) build $(GOFLAGS) -o $(BINARY_NAME) cmd/server/main.go

test: ## Run all tests
	@echo "Running tests..."
	@$(GO) test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-unit: ## Run unit tests only (exclude integration tests)
	@echo "Running unit tests..."
	@$(GO) test -v -short ./...

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	@$(GO) test -v ./tests/integration_tests/...

lint: ## Run golangci-lint
	@echo "Running linters..."
	@golangci-lint run --config=.golangci.yml

lint-fix: ## Run golangci-lint with auto-fix
	@echo "Running linters with auto-fix..."
	@golangci-lint run --config=.golangci.yml --fix

fmt: ## Run gofmt
	@echo "Formatting code..."
	@gofmt -s -w .
	@$(GO) fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	@$(GO) vet ./...

install-deps: ## Install development dependencies
	@echo "Installing dependencies..."
	@$(GO) mod download
	@$(GO) mod tidy
	@echo "Installing golangci-lint..."
	@which golangci-lint || (go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@echo "Installing pre-commit..."
	@which pre-commit || (brew install pre-commit || pip install pre-commit)

pre-commit-install: ## Install pre-commit hooks
	@echo "Installing pre-commit hooks..."
	@pre-commit install

pre-commit-run: ## Run pre-commit hooks manually
	@echo "Running pre-commit hooks..."
	@pre-commit run --all-files

mod-tidy: ## Tidy go.mod
	@echo "Tidying go.mod..."
	@$(GO) mod tidy

mod-verify: ## Verify dependencies
	@echo "Verifying dependencies..."
	@$(GO) mod verify

sandbox-start: ## Start sandbox server (agent-infra/sandbox)
	@echo "Starting sandbox server on port $(SANDBOX_PORT)..."
	@docker run --security-opt seccomp=unconfined --rm -it -p $(SANDBOX_PORT):8080 $(DOCKER_REGISTRY):latest

sandbox-start-cn: ## Start sandbox server for China users
	@echo "Starting sandbox server (China) on port $(SANDBOX_PORT)..."
	@docker run --security-opt seccomp=unconfined --rm -it -p $(SANDBOX_PORT):8080 enterprise-public-cn-beijing.cr.volces.com/vefaas-public/all-in-one-sandbox:latest

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@rm -f coverage.out coverage.html
	@rm -rf logs/*.log

all: fmt vet lint test ## Run fmt, vet, lint and test

.DEFAULT_GOAL := help
