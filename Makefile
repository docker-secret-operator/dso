.PHONY: help ui-build build test release clean all verify-assets

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
RED := \033[0;31m
NC := \033[0m # No Color

# Variables
DSO_BINARY := cmd/dso
VERSION ?= dev
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

help:
	@echo "$(BLUE)DSO Build System$(NC)"
	@echo ""
	@echo "Available targets:"
	@echo "  $(GREEN)make ui-build$(NC)         - Build Next.js frontend assets"
	@echo "  $(GREEN)make build$(NC)            - Build Go binary with embedded assets"
	@echo "  $(GREEN)make test$(NC)             - Run all tests"
	@echo "  $(GREEN)make release$(NC)          - Create release artifacts"
	@echo "  $(GREEN)make verify-assets$(NC)    - Verify asset pipeline"
	@echo "  $(GREEN)make clean$(NC)            - Clean build artifacts"
	@echo "  $(GREEN)make all$(NC)              - Build everything (ui-build + build + test)"

# Step 1: Build Next.js frontend and copy to Go
ui-build:
	@echo "$(BLUE)[UI Build] Building Next.js frontend...$(NC)"
	@cd web && npm install --prefer-offline --no-audit
	@cd web && npm run build
	@echo "$(BLUE)[UI Build] Copying assets to embedding location...$(NC)"
	@rm -rf internal/webui/assets
	@mkdir -p internal/webui/assets
	@cp -r web/out/* internal/webui/assets/
	@echo "$(BLUE)[UI Build] Verifying assets exist...$(NC)"
	@[ -f internal/webui/assets/index.html ] || (echo "$(RED)ERROR: index.html not found$(NC)" && exit 1)
	@[ -d internal/webui/assets/_next ] || (echo "$(RED)ERROR: _next directory not found$(NC)" && exit 1)
	@echo "$(GREEN)✓ UI build complete$(NC)"

# Verify assets are valid before building Go binary
verify-assets:
	@echo "$(BLUE)[Verify] Checking asset pipeline...$(NC)"
	@[ -d internal/webui/assets ] || (echo "$(RED)ERROR: internal/webui/assets not found. Run 'make ui-build' first$(NC)" && exit 1)
	@[ -f internal/webui/assets/index.html ] || (echo "$(RED)ERROR: Missing index.html$(NC)" && exit 1)
	@[ -d internal/webui/assets/_next ] || (echo "$(RED)ERROR: Missing _next directory$(NC)" && exit 1)
	@echo "$(BLUE)[Verify] Checking embed.go directive...$(NC)"
	@grep -q "//go:embed assets/\*" internal/webui/embed.go || (echo "$(RED)ERROR: Invalid embed directive in embed.go$(NC)" && exit 1)
	@echo "$(GREEN)✓ Asset pipeline verified$(NC)"

# Step 2: Build Go binary with embedded assets
build: verify-assets
	@echo "$(BLUE)[Build] Running gofmt...$(NC)"
	@gofmt -w ./internal/webui ./internal/cli/ui.go
	@echo "$(BLUE)[Build] Running go vet...$(NC)"
	@go vet ./cmd/... ./internal/... ./pkg/...
	@echo "$(BLUE)[Build] Building DSO binary...$(NC)"
	@go build \
		-ldflags="-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)" \
		-o dso \
		./cmd/dso
	@echo "$(GREEN)✓ Build complete: dso ($(shell ls -lh dso | awk '{print $$5}'))$(NC)"

# Step 3: Run tests
test:
	@echo "$(BLUE)[Test] Running Go tests...$(NC)"
	@go test -v -race ./...
	@echo "$(BLUE)[Test] Running Go test coverage...$(NC)"
	@go test -coverprofile=coverage.out ./...
	@echo "$(GREEN)✓ Tests complete$(NC)"

# Step 4: Create release artifacts
release: clean ui-build build test
	@echo "$(BLUE)[Release] Creating release directory...$(NC)"
	@mkdir -p release
	@echo "$(BLUE)[Release] Copying binary...$(NC)"
	@cp dso release/dso-$(VERSION)
	@echo "$(BLUE)[Release] Creating checksum...$(NC)"
	@cd release && sha256sum dso-$(VERSION) > dso-$(VERSION).sha256
	@echo "$(BLUE)[Release] Creating release metadata...$(NC)"
	@echo "Version: $(VERSION)" > release/MANIFEST.txt
	@echo "Build Time: $(BUILD_TIME)" >> release/MANIFEST.txt
	@echo "Git Commit: $(GIT_COMMIT)" >> release/MANIFEST.txt
	@echo "Binary Size: $(shell ls -lh release/dso-$(VERSION) | awk '{print $$5}')" >> release/MANIFEST.txt
	@echo "$(GREEN)✓ Release artifacts created in ./release/$(NC)"

# Full build
all: ui-build build test
	@echo "$(GREEN)✓ All builds complete$(NC)"

# Clean
clean:
	@echo "$(BLUE)[Clean] Removing build artifacts...$(NC)"
	@rm -f dso coverage.out
	@rm -rf release/
	@echo "$(GREEN)✓ Clean complete$(NC)"

# Watch mode (for development)
watch-ui:
	@cd web && npm run dev
