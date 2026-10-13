# Cloudtop Build Configuration
BINARY_NAME := cloudtop
MODULE := github.com/afterdarksys/opskit/cloudtop
MAIN_PKG := ./cmd/cloudtop

# Build info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go settings
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(BUILD_DATE)

# Output directory
BUILD_DIR := build

# Platforms for cross-compilation
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build clean install test lint deps run help

## Default target
all: build

## Build the binary for current platform
build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(MAIN_PKG)

## Build for all platforms
build-all: $(PLATFORMS)

$(PLATFORMS):
	$(eval OS := $(word 1,$(subst /, ,$@)))
	$(eval ARCH := $(word 2,$(subst /, ,$@)))
	$(eval EXT := $(if $(filter windows,$(OS)),.exe,))
	@mkdir -p $(BUILD_DIR)
	GOOS=$(OS) GOARCH=$(ARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(OS)-$(ARCH)$(EXT) $(MAIN_PKG)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-$(OS)-$(ARCH)$(EXT)"

## Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -rf $(BUILD_DIR)
	$(GO) clean

## Install to GOPATH/bin
install:
	$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS)" $(MAIN_PKG)

## Run tests
test:
	$(GO) test -v -race ./...

## Run tests with coverage
test-cover:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

## Tidy and download dependencies
deps:
	$(GO) mod tidy
	$(GO) mod download

## Run the application
run: build
	./$(BINARY_NAME)

## Show help
help:
	@echo "Cloudtop Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build       Build binary for current platform"
	@echo "  build-all   Build binaries for all platforms"
	@echo "  clean       Remove build artifacts"
	@echo "  install     Install to GOPATH/bin"
	@echo "  test        Run tests"
	@echo "  test-cover  Run tests with coverage report"
	@echo "  lint        Run golangci-lint"
	@echo "  deps        Tidy and download dependencies"
	@echo "  run         Build and run the application"
	@echo "  help        Show this help"
