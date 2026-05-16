# Mcaster1StreamProxy — Go ICY Stream Proxy
# Owner: MCaster1 LLC / David St John <davestj@mcaster1.com>
#
# Targets:
#   make              — build the binary
#   make clean        — remove build artifacts
#   make install      — install to /usr/local/mcaster1/bin/
#   make test         — run unit tests
#   make run          — build and run with local config
#   make fmt          — format Go source
#   make vet          — static analysis

BINARY      := mcaster1-stream-proxy
BUILD_DIR   := build
SRC_DIR     := cmd/mcaster1-stream-proxy
CONFIG      := etc/config.yaml
INSTALL_DIR := /usr/local/mcaster1

VERSION     := 1.0.0
BUILD_TIME  := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS     := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -s -w"

GO          := go

.PHONY: all build clean install test run fmt vet deps

all: build

build: deps
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./$(SRC_DIR)
	@echo "Built: $(BUILD_DIR)/$(BINARY)"
	@ls -lh $(BUILD_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)
	$(GO) clean -cache

deps:
	$(GO) mod download
	$(GO) mod tidy

test:
	$(GO) test -v -race ./internal/...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

run: build
	$(BUILD_DIR)/$(BINARY) -c $(CONFIG)

install: build
	@echo "Installing to $(INSTALL_DIR)..."
	install -d $(INSTALL_DIR)/bin
	install -d $(INSTALL_DIR)/etc
	install -d $(INSTALL_DIR)/var/log/stream-proxy
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/bin/$(BINARY)
	@if [ ! -f $(INSTALL_DIR)/etc/stream-proxy.yaml ]; then \
		install -m 0640 $(CONFIG) $(INSTALL_DIR)/etc/stream-proxy.yaml; \
		echo "Installed config: $(INSTALL_DIR)/etc/stream-proxy.yaml"; \
	else \
		echo "Config exists, not overwriting: $(INSTALL_DIR)/etc/stream-proxy.yaml"; \
	fi
	@echo "Installed: $(INSTALL_DIR)/bin/$(BINARY)"

# Development: build + run with hot-reload awareness
dev: build
	@echo "Starting in dev mode..."
	$(BUILD_DIR)/$(BINARY) -c $(CONFIG)
