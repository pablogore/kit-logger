.PHONY: help test build clean fmt lint vet coverage coverage-html coverage-func coverage-complete deps security check example

# Default target
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build the logger library
build: ## Build the go-kit-logger library
	go build ./...

# Run tests
test: ## Run all tests
	go test -v ./...

# Run tests with coverage
coverage: ## Run tests with coverage report
	@mkdir -p coverage
	go test -coverprofile=coverage/coverage.out ./...
	@echo "Coverage report generated: coverage/coverage.out"

# Generate HTML coverage report
coverage-html: coverage ## Generate HTML coverage report
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "HTML coverage report generated: coverage/coverage.html"

# Show coverage function breakdown
coverage-func: coverage ## Show coverage function breakdown
	go tool cover -func=coverage/coverage.out

# Generate complete coverage report with detailed analysis
coverage-complete: ## Generate complete coverage report with threshold validation and detailed analysis
	@echo "Generating complete coverage report with 85% threshold validation..."
	@./scripts/coverage-complete-report.sh

# Format code
fmt: ## Format Go code
	go fmt ./...

# Lint code
lint: ## Lint Go code
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		echo "Skipping lint..."; \
	fi

# Vet code
vet: ## Vet Go code
	go vet ./...

# Clean build artifacts
clean: ## Clean build artifacts
	go clean
	rm -rf coverage/
	rm -rf dist/

# Install dependencies
deps: ## Install dependencies
	go mod download
	go mod tidy

# Run basic example
example: ## Run a basic logging example
	@echo "Running basic logging example..."
	@go run -c 'package main; import "github.com/pablogore/kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "info", Format: "json"}); log.Info("Hello from kit-logger!"); }'

# Run all examples
examples: ## Run all examples
	@echo "Running basic example..."
	@go run -c 'package main; import "github.com/pablogore/kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "info", Format: "json"}); log.Info("Basic example"); }'
	@echo ""
	@echo "Running structured logging example..."
	@go run -c 'package main; import "github.com/pablogore/kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "debug", Format: "text"}); log.Info("User login", "user_id", "123", "ip", "192.168.1.1"); }'

# Check for security vulnerabilities
security: ## Check for security vulnerabilities
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not found. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
		echo "Skipping security check..."; \
	fi

# All checks
check: fmt lint vet test ## Run all checks (fmt, lint, vet, test)

# Full test suite with coverage
test-full: test coverage-func coverage-html ## Run full test suite with coverage

# Complete test suite with detailed coverage analysis
test-complete: test coverage-complete ## Run complete test suite with detailed coverage analysis

# Development setup
dev-setup: deps ## Setup development environment
	@echo "Development environment setup complete"
	@echo "Installed dependencies"

# CI/CD pipeline
ci: deps fmt lint vet test coverage-func ## Run CI/CD pipeline
	@echo "CI/CD pipeline completed successfully"

# CI/CD pipeline with threshold validation
ci-threshold: deps fmt lint vet test coverage-threshold ## Run CI/CD pipeline with threshold validation
	@echo "CI/CD pipeline with threshold validation completed successfully"

# Coverage with threshold validation (85% minimum)
coverage-threshold: ## Generate coverage report with 85% threshold validation
	@echo "Generating coverage report with 85% threshold validation..."
	@./scripts/coverage-complete-report.sh

# Complete CI/CD pipeline with detailed coverage
ci-complete: deps fmt lint vet test coverage-threshold ## Run complete CI/CD pipeline with detailed coverage analysis and threshold validation
	@echo "Complete CI/CD pipeline completed successfully"

# Build for release
release: clean build test ## Build for release (clean, build, test)
	@echo "Release build completed successfully"

# Show project info
info: ## Show project information
	@echo "=== Kit Logger Project Info ==="
	@echo "Module: github.com/pablogore/kit-logger"
	@echo "Go version: $(shell go version)"
	@echo "Number of test files: $(shell find ./pkg -name "*_test.go" -not -path "./.git/*" | wc -l | tr -d ' ')"
	@echo "Test files:"
	@find ./pkg -name "*_test.go" -not -path "./.git/*" | sed 's/^/  /'
	@echo ""
	@echo "Available packages:"
	@echo "  - pkg/logger: Main logging framework"
	@echo "  - pkg/logger/handler: Log handlers (filtering, sampling, etc.)"
	@echo "  - pkg/logger/grpc: gRPC interceptors"
	@echo "  - pkg/logger/httpmw: HTTP middleware"
	@echo "  - pkg/logger/utils: Utility functions"
	@echo "  - pkg/logger/kitlogtest: Test doubles (MockLogger, TestHandler, etc.) for consumers of this module"
	@echo ""
	@echo "Features:"
	@echo "  - Structured logging with slog"
	@echo "  - Prometheus metrics integration"
	@echo "  - Configurable handlers (filtering, sampling, buffering)"
	@echo "  - HTTP and gRPC middleware"
	@echo "  - Component-based logging"
	@echo "  - Global fields support"
	@echo "  - Comprehensive test coverage"

# Quick test (fast)
test-quick: ## Run tests without verbose output
	go test ./...

# Test specific package
test-pkg: ## Test specific package (usage: make test-pkg PKG=./pkg/logger/handler)
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-pkg PKG=./path/to/package"; \
		exit 1; \
	fi
	go test -v $(PKG)

# Benchmark tests
bench: ## Run benchmark tests
	go test -bench=. -benchmem ./...

# Race condition tests
test-race: ## Run tests with race condition detection
	go test -race ./...

# Generate documentation
docs: ## Generate documentation
	@echo "Generating documentation..."
	@if command -v godoc >/dev/null 2>&1; then \
		echo "Starting godoc server on http://localhost:6060"; \
		echo "Press Ctrl+C to stop"; \
		godoc -http=:6060; \
	else \
		echo "godoc not found. Install with: go install golang.org/x/tools/cmd/godoc@latest"; \
	fi
