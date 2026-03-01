.PHONY: help test build clean fmt lint vet mocks coverage coverage-html coverage-func coverage-complete deps security check example

# Default target
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build the logger library
build: ## Build the go-kit-logger library (excluding mocks)
	go build ./pkg/...

# Run tests
test: ## Run all tests (excluding mocks)
	go test -v ./pkg/...

# Run tests including mocks
test-all: ## Run all tests including mocks
	go test -v ./...

# Run tests with coverage
coverage: ## Run tests with coverage report (excluding mocks)
	@mkdir -p coverage
	go test -coverprofile=coverage/coverage.out ./pkg/...
	@echo "Coverage report generated: coverage/coverage.out"

# Run tests with coverage including mocks
coverage-all: ## Run tests with coverage report including mocks
	@mkdir -p coverage
	go test -coverprofile=coverage/coverage.out ./...
	@echo "Coverage report generated: coverage/coverage.out"

# Run tests with coverage (excluding examples and mocks)
coverage-core: ## Run tests with coverage report (excluding examples and mocks)
	@mkdir -p coverage
	go test -v -coverprofile=coverage/coverage.out -coverpkg=./pkg/... ./pkg/...
	@echo "Core coverage report generated: coverage/coverage.out"

# Generate HTML coverage report
coverage-html: coverage ## Generate HTML coverage report
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "HTML coverage report generated: coverage/coverage.html"

# Generate HTML coverage report (core only)
coverage-html-core: coverage-core ## Generate HTML coverage report (core only)
	go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Core HTML coverage report generated: coverage/coverage.html"

# Show coverage function breakdown
coverage-func: coverage ## Show coverage function breakdown
	go tool cover -func=coverage/coverage.out

# Generate complete coverage report with detailed analysis
coverage-complete: ## Generate complete coverage report with threshold validation and detailed analysis
	@echo "Generating complete coverage report with 85% threshold validation..."
	@./scripts/coverage-complete-report.sh

# Generate mocks
mocks: ## Generate mocks using testify/mock
	@echo "Generating mocks..."
	@echo "Mocks are already generated manually in the mocks/ directory"
	@echo "To regenerate specific mocks, edit the corresponding files in mocks/"

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
	@go run -c 'package main; import "github.com/getsyntegrity/go-kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "info", Format: "json"}); log.Info("Hello from go-kit-logger!"); }'

# Run all examples
examples: ## Run all examples
	@echo "Running basic example..."
	@go run -c 'package main; import "github.com/getsyntegrity/go-kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "info", Format: "json"}); log.Info("Basic example"); }'
	@echo ""
	@echo "Running structured logging example..."
	@go run -c 'package main; import "github.com/getsyntegrity/go-kit-logger/pkg/logger"; func main() { log := logger.New(logger.Config{Level: "debug", Format: "text"}); log.Info("User login", "user_id", "123", "ip", "192.168.1.1"); }'

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
test-full: mocks test coverage-func coverage-html ## Run full test suite with mocks and coverage

# Complete test suite with detailed coverage analysis
test-complete: mocks test coverage-complete ## Run complete test suite with detailed coverage analysis

# Development setup
dev-setup: deps mocks ## Setup development environment
	@echo "Development environment setup complete"
	@echo "Installed dependencies and generated mocks"

# CI/CD pipeline
ci: deps mocks fmt lint vet test coverage-func ## Run CI/CD pipeline
	@echo "CI/CD pipeline completed successfully"

# CI/CD pipeline with threshold validation
ci-threshold: deps mocks fmt lint vet test coverage-threshold ## Run CI/CD pipeline with threshold validation
	@echo "CI/CD pipeline with threshold validation completed successfully"

# Coverage with threshold validation (85% minimum)
coverage-threshold: ## Generate coverage report with 85% threshold validation
	@echo "Generating coverage report with 85% threshold validation..."
	@./scripts/coverage-complete-report.sh

# Complete CI/CD pipeline with detailed coverage
ci-complete: deps mocks fmt lint vet test coverage-threshold ## Run complete CI/CD pipeline with detailed coverage analysis and threshold validation
	@echo "Complete CI/CD pipeline completed successfully"

# Build for release
release: clean build test ## Build for release (clean, build, test)
	@echo "Release build completed successfully"

# Show project info
info: ## Show project information
	@echo "=== Go Kit Logger Project Info ==="
	@echo "Module: github.com/getsyntegrity/go-kit-logger"
	@echo "Go version: $(shell go version)"
	@echo "Number of test files (excluding mocks): $(shell find ./pkg -name "*_test.go" -not -path "./.git/*" | wc -l | tr -d ' ')"
	@echo "Test files (excluding mocks):"
	@find ./pkg -name "*_test.go" -not -path "./.git/*" | sed 's/^/  /'
	@echo ""
	@echo "Available packages:"
	@echo "  - pkg/logger: Main logging framework"
	@echo "  - pkg/logger/handler: Log handlers (filtering, sampling, etc.)"
	@echo "  - pkg/logger/grpc: gRPC interceptors"
	@echo "  - pkg/logger/httpmw: HTTP middleware"
	@echo "  - pkg/logger/utils: Utility functions"
	@echo "  - mocks/: Complete mock library for testing"
	@echo ""
	@echo "Features:"
	@echo "  - Structured logging with slog"
	@echo "  - Prometheus metrics integration"
	@echo "  - Configurable handlers (filtering, sampling, buffering)"
	@echo "  - HTTP and gRPC middleware"
	@echo "  - Component-based logging"
	@echo "  - Global fields support"
	@echo "  - Comprehensive test coverage"

# Show mock info
mocks-info: ## Show information about available mocks
	@echo "=== Available Mocks ==="
	@echo "Main package mocks:"
	@echo "  - Logger: Complete logger interface mock"
	@echo "  - ContextFieldExtractorFunc: Context field extraction mock"
	@echo ""
	@echo "Handler mocks:"
	@echo "  - FilterRule: Log filtering rules"
	@echo "  - SamplingConfig: Sampling configuration"
	@echo "  - BufferedHandler: Asynchronous buffering"
	@echo "  - HookHandler: Pre-processing hooks"
	@echo "  - MultiHandler: Multiple handler composition"
	@echo ""
	@echo "HTTP/gRPC mocks:"
	@echo "  - HTTP Middleware: Request logging middleware"
	@echo "  - gRPC Interceptor: Unary call logging"
	@echo ""
	@echo "Utils mocks:"
	@echo "  - LogUtils: Log record utilities"
	@echo "  - SlogRecord: Mock slog.Record for testing"
	@echo ""
	@echo "Usage: import 'github.com/getsyntegrity/go-kit-logger/mocks'"

# Quick test (fast)
test-quick: ## Run tests without verbose output (excluding mocks)
	go test ./pkg/...

# Test specific package
test-pkg: ## Test specific package (usage: make test-pkg PKG=./pkg/logger/handler)
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-pkg PKG=./path/to/package"; \
		exit 1; \
	fi
	go test -v $(PKG)

# Benchmark tests
bench: ## Run benchmark tests (excluding mocks)
	go test -bench=. -benchmem ./pkg/...

# Race condition tests
test-race: ## Run tests with race condition detection (excluding mocks)
	go test -race ./pkg/...

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


