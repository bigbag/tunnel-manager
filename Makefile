.PHONY: all build test clean install tidy run run/service run/quick vet build-all help fmt lint coverage test-race coverage-html sys/changelog sys/tag

BINARY=tunnel-manager
BUILD_DIR=bin
LIST ?= mcp
ARGS ?=

GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet
GOFMT=$(GOCMD) fmt

all: help

## Service:

run/service: build ## Start the tunnel service. Set LIST (default mcp) and ARGS
	@./$(BUILD_DIR)/$(BINARY) $(LIST) $(ARGS)

run: run/service ## Build and start the tunnel service

run/quick: ## Start the tunnel service without a rebuild
	@./$(BUILD_DIR)/$(BINARY) $(LIST) $(ARGS)

## Development:

build: tidy ## Build the binary
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY) ./cmd/tunnel-manager

test: ## Run tests
	$(GOTEST) -v ./...

test-race: ## Run tests with race detection
	$(GOTEST) -race -v ./...

coverage: ## Run tests with coverage report
	$(GOTEST) -cover ./...

coverage-html: ## Generate HTML coverage report
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt: ## Format code
	$(GOFMT) ./...

vet: ## Run go vet
	$(GOVET) ./...

lint: fmt vet ## Run fmt and vet

tidy: ## Tidy dependencies
	$(GOMOD) tidy

clean: ## Clean build artifacts
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY)
	rm -f coverage.out coverage.html

## Installation:

install: tidy ## Install to GOPATH/bin
	$(GOCMD) install ./cmd/tunnel-manager

build-all: tidy ## Build for linux/darwin/windows amd64/arm64
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/tunnel-manager
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/tunnel-manager
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/tunnel-manager
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/tunnel-manager
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/tunnel-manager
	GOOS=windows GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY)-windows-arm64.exe ./cmd/tunnel-manager

## System:

sys/changelog: ## Generate changelog from git tags
	@echo "Generating CHANGELOG.md..."
	@echo "" > CHANGELOG.md
	@previous_tag=0; \
	for current_tag in $$(git tag --sort=-creatordate | grep '^v'); do \
		if [ "$$previous_tag" != 0 ]; then \
			tag_date=$$(git log -1 --pretty=format:'%ad' --date=short $$previous_tag); \
			printf "\n## $$previous_tag ($$tag_date)\n\n" >> CHANGELOG.md; \
			git log $$current_tag...$$previous_tag --pretty=format:'*  %s [%an]' --reverse | grep -v Merge >> CHANGELOG.md; \
			printf "\n" >> CHANGELOG.md; \
		fi; \
		previous_tag=$$current_tag; \
	done
	@echo "CHANGELOG.md generated successfully."

sys/tag: ## Create and push version tag
	@read -p "Enter tag version (e.g., 1.0.0): " TAG; \
	if echo "$$TAG" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$'; then \
		git tag -a v$$TAG -m v$$TAG; \
		git push origin v$$TAG; \
		echo "Tag v$$TAG created and pushed successfully."; \
	else \
		echo "Invalid tag format. Please use X.Y.Z (e.g., 1.0.0)"; \
		exit 1; \
	fi

## Help:

help: ## Show this help
	@echo "tunnel-manager - SSH and kubectl tunnel runner"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "  make run/service"
	@echo "  make run/service LIST=mcp ARGS='--check-interval 60'"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"; section=""} \
		/^##/ { section=substr($$0, 4); next } \
		/^[a-zA-Z_\/-]+:.*##/ { \
			if (section != "") { printf "\n\033[1m%s\033[0m\n", section; section="" } \
			printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 \
		}' $(MAKEFILE_LIST)
	@echo ""
