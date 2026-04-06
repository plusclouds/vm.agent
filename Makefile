# PlusClouds Ubuntu Agent Makefile

BINARY_AGENT   := plusclouds-agent
BINARY_CTL     := plsctl
CMD_AGENT      := ./cmd/agent
CMD_CTL        := ./cmd/ctl
BUILD_DIR      := ./bin
PROTO_DIR      := ./proto
PROTO_OUT      := ./internal/api/grpc/pb

VERSION        ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS        := -ldflags "-X github.com/plusclouds/ubuntu-agent/internal/config.AgentVersion=$(VERSION)"

INSTALL_DIR    := /usr/local/bin
SERVICE_DIR    := /etc/systemd/system
CONFIG_DIR     := /etc/plusclouds

.PHONY: all build build-agent build-ctl proto test lint clean install package-deb

## all: build both binaries
all: build

## build: build both agent and ctl binaries
build: build-agent build-ctl

## build-agent: build the agent daemon
build-agent:
	@echo "Building $(BINARY_AGENT)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_AGENT) $(CMD_AGENT)
	@echo "Done: $(BUILD_DIR)/$(BINARY_AGENT)"

## build-ctl: build the plsctl CLI
build-ctl:
	@echo "Building $(BINARY_CTL)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_CTL) $(CMD_CTL)
	@echo "Done: $(BUILD_DIR)/$(BINARY_CTL)"

## proto: generate protobuf/gRPC code
proto:
	@echo "Generating protobuf code..."
	@mkdir -p $(PROTO_OUT)
	protoc \
		--go_out=$(PROTO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(PROTO_OUT) \
		--go-grpc_opt=paths=source_relative \
		-I$(PROTO_DIR) \
		$(PROTO_DIR)/*.proto
	@echo "Proto generation done."

## test: run all tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: run golangci-lint
lint:
	@echo "Running linter..."
	golangci-lint run ./...

## clean: remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean done."

## install: install binaries and systemd unit
install: build
	@echo "Installing $(BINARY_AGENT) to $(INSTALL_DIR)..."
	install -m 0755 $(BUILD_DIR)/$(BINARY_AGENT) $(INSTALL_DIR)/$(BINARY_AGENT)
	@echo "Installing $(BINARY_CTL) to $(INSTALL_DIR)..."
	install -m 0755 $(BUILD_DIR)/$(BINARY_CTL) $(INSTALL_DIR)/$(BINARY_CTL)
	@echo "Installing systemd unit..."
	install -m 0644 systemd/plusclouds-agent.service $(SERVICE_DIR)/plusclouds-agent.service
	@echo "Installing default config..."
	@mkdir -p $(CONFIG_DIR)
	@if [ ! -f $(CONFIG_DIR)/agent.yaml ]; then \
		install -m 0640 configs/agent.yaml $(CONFIG_DIR)/agent.yaml; \
		echo "Installed default config to $(CONFIG_DIR)/agent.yaml"; \
	else \
		echo "Config already exists at $(CONFIG_DIR)/agent.yaml, skipping."; \
	fi
	systemctl daemon-reload
	@echo "Install complete. Run: systemctl enable --now plusclouds-agent"

## package-deb: build a .deb package
package-deb: build
	@echo "Packaging .deb..."
	@mkdir -p dist/deb/DEBIAN
	@mkdir -p dist/deb/usr/local/bin
	@mkdir -p dist/deb/etc/systemd/system
	@mkdir -p dist/deb/etc/plusclouds
	cp $(BUILD_DIR)/$(BINARY_AGENT) dist/deb/usr/local/bin/
	cp $(BUILD_DIR)/$(BINARY_CTL) dist/deb/usr/local/bin/
	cp systemd/plusclouds-agent.service dist/deb/etc/systemd/system/
	cp configs/agent.yaml dist/deb/etc/plusclouds/agent.yaml
	@printf "Package: plusclouds-agent\nVersion: $(VERSION)\nSection: utils\nPriority: optional\nArchitecture: amd64\nMaintainer: PlusClouds <support@plusclouds.com>\nDescription: PlusClouds Ubuntu VM Agent\n A daemon and CLI for managing PlusClouds VMs.\n" > dist/deb/DEBIAN/control
	@printf "#!/bin/sh\nsystemctl daemon-reload\nsystemctl enable plusclouds-agent\n" > dist/deb/DEBIAN/postinst
	@chmod 0755 dist/deb/DEBIAN/postinst
	dpkg-deb --build dist/deb dist/plusclouds-agent_$(VERSION)_amd64.deb
	@echo "Package built: dist/plusclouds-agent_$(VERSION)_amd64.deb"

## help: display this help
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
