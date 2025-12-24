.PHONY: help test test-verbose test-coverage build clean fmt vet lint install-tools

# Colors for output
GREEN  := \033[0;32m
YELLOW := \033[0;33m
RED    := \033[0;31m
NC     := \033[0m # No Color

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  ${GREEN}%-15s${NC} %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run tests
	@echo "${GREEN}Running tests...${NC}"
	@go test -v -race ./...

test-verbose: ## Run tests with verbose output
	@echo "${GREEN}Running tests (verbose)...${NC}"
	@go test -v -race -count=1 ./...

test-coverage: ## Run tests with coverage report
	@echo "${GREEN}Running tests with coverage...${NC}"
	@go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "${GREEN}Coverage summary:${NC}"
	@go tool cover -func=coverage.out
	@echo "\n${YELLOW}Generating HTML coverage report...${NC}"
	@go tool cover -html=coverage.out -o coverage.html
	@echo "${GREEN}Coverage report: coverage.html${NC}"

build: ## Build the module
	@echo "${GREEN}Building registry-client-v2...${NC}"
	@go build -v ./...

clean: ## Clean build artifacts and test cache
	@echo "${YELLOW}Cleaning...${NC}"
	@go clean -testcache
	@rm -f coverage.out coverage.html
	@echo "${GREEN}Clean complete${NC}"

fmt: ## Format code
	@echo "${GREEN}Formatting code...${NC}"
	@gofmt -w -s .
	@echo "${GREEN}Format complete${NC}"

fmt-check: ## Check code formatting
	@echo "${GREEN}Checking code format...${NC}"
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "${RED}The following files are not formatted:${NC}"; \
		gofmt -l .; \
		exit 1; \
	else \
		echo "${GREEN}All files are properly formatted${NC}"; \
	fi

vet: ## Run go vet
	@echo "${GREEN}Running go vet...${NC}"
	@go vet ./...
	@echo "${GREEN}Vet complete${NC}"

lint: install-tools ## Run golangci-lint
	@echo "${GREEN}Running golangci-lint...${NC}"
	@golangci-lint run ./...
	@echo "${GREEN}Lint complete${NC}"

staticcheck: install-tools ## Run staticcheck
	@echo "${GREEN}Running staticcheck...${NC}"
	@staticcheck ./...
	@echo "${GREEN}Staticcheck complete${NC}"

bench: ## Run benchmarks
	@echo "${GREEN}Running benchmarks...${NC}"
	@go test -bench=. -benchmem ./...

install-tools: ## Install development tools
	@echo "${GREEN}Installing development tools...${NC}"
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "${GREEN}Tools installed${NC}"

mod-tidy: ## Tidy go.mod
	@echo "${GREEN}Tidying go.mod...${NC}"
	@go mod tidy
	@echo "${GREEN}Tidy complete${NC}"

mod-verify: ## Verify dependencies
	@echo "${GREEN}Verifying dependencies...${NC}"
	@go mod verify
	@echo "${GREEN}Verification complete${NC}"

check: fmt-check vet test ## Run all checks (format, vet, test)
	@echo "${GREEN}All checks passed!${NC}"

ci: clean fmt-check vet test-coverage build ## Run CI pipeline locally
	@echo "${GREEN}CI pipeline complete!${NC}"

watch-test: ## Watch for changes and run tests
	@echo "${YELLOW}Watching for changes (requires 'entr')...${NC}"
	@find . -name '*.go' | entr -c make test

# Example usage target
example: ## Show example usage
	@echo "${GREEN}Registry Client V2 - Example Usage${NC}"
	@echo ""
	@echo "1. Import the package:"
	@echo '   import registryclient "registry-client-v2"'
	@echo ""
	@echo "2. Create a client:"
	@echo '   client := registryclient.NewRegistryClient('
	@echo '       "go-proxy:9090", "my-service", "", 3001, metadata, true)'
	@echo ""
	@echo "3. Initialize and register routes:"
	@echo '   client.Init()'
	@echo '   client.AddRoute([]string{"example.com"}, "/", "http://...", 10)'
	@echo '   client.ApplyConfig()'
	@echo ""
	@echo "See README.md for full documentation"
