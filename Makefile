BINARY   := docker-dso
MODULE   := github.com/docker-secret-operator/dso
CMD      := ./cmd/dso
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -X main.version=$(VERSION) -s -w

GOBIN   ?= $(shell go env GOPATH)/bin
PLUGIN_DIR_SYSTEM := /usr/local/lib/docker/cli-plugins
BIN_DIR_SYSTEM    := /usr/local/bin
PLUGIN_DIR_USER   := $(HOME)/.docker/cli-plugins
BIN_DIR_USER      := $(HOME)/.local/bin

.PHONY: all build test lint clean install install-user fmt vet

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "golangci-lint not found — install via: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./...

install: build
	@echo "Installing to system paths (requires sudo)..."
	sudo install -Dm755 $(BINARY) $(PLUGIN_DIR_SYSTEM)/$(BINARY)
	sudo install -Dm755 $(BINARY) $(BIN_DIR_SYSTEM)/$(BINARY)
	sudo ln -sf $(BIN_DIR_SYSTEM)/$(BINARY) $(BIN_DIR_SYSTEM)/dso
	@echo "Installed: docker dso (plugin) and dso (symlink)"

install-user: build
	@echo "Installing to user paths..."
	mkdir -p $(PLUGIN_DIR_USER) $(BIN_DIR_USER)
	install -Dm755 $(BINARY) $(PLUGIN_DIR_USER)/$(BINARY)
	install -Dm755 $(BINARY) $(BIN_DIR_USER)/$(BINARY)
	ln -sf $(BIN_DIR_USER)/$(BINARY) $(BIN_DIR_USER)/dso
	@echo "Installed: docker dso (plugin) and dso (symlink) — ensure $(BIN_DIR_USER) is in PATH"

clean:
	rm -f $(BINARY)
	rm -f docker-compose-dso-*.yaml
