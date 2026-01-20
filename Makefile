.PHONY: all build generate clean test run-daemon install-tools dev install-air install

# Binary output directory
BIN_DIR := bin

# Proto paths
PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto')

all: generate build

# Build all binaries
build: build-map build-mapd

build-map:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/map ./cmd/map

build-mapd:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/mapd ./cmd/mapd

# Generate protobuf code
generate:
	@mkdir -p proto/map/v1
	protoc -Iproto --go_out=proto --go_opt=paths=source_relative \
		--go-grpc_out=proto --go-grpc_opt=paths=source_relative \
		map/v1/types.proto map/v1/daemon.proto

# Install required tools
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	find $(PROTO_DIR) -name '*.pb.go' -delete

# Run tests
test:
	go test -v ./...

# Development helpers
run-daemon:
	go run ./cmd/mapd

run-cli:
	go run ./cmd/map $(ARGS)

# Quick iteration: generate + build
rebuild: generate build

# Download dependencies
deps:
	go mod download
	go mod tidy

# Install air for hot reloading
install-air:
	@command -v air >/dev/null 2>&1 || { \
		echo "Installing air..."; \
		go install github.com/air-verse/air@latest; \
	}

# Development mode with hot reloading
# Watches for changes, rebuilds, and copies binaries to ~/.local/bin
dev: install-air
	@mkdir -p ~/.local/bin
	@rm -rf tmp
	@echo "Starting development mode with hot reloading..."
	@echo "Binaries will be copied to ~/.local/bin on each rebuild"
	@echo "Press Ctrl+C to stop"
	@air -c .air.toml

# Build and copy to ~/.local/bin (used by air for hot reload)
dev-build: build
	@mkdir -p ~/.local/bin
	@cp $(BIN_DIR)/map $(BIN_DIR)/mapd ~/.local/bin/
	@echo "Binaries copied to ~/.local/bin"

# Install binaries to ~/.local/bin (one-time)
install: build
	@mkdir -p ~/.local/bin
	cp $(BIN_DIR)/map $(BIN_DIR)/mapd ~/.local/bin/
	@echo "Installed map and mapd to ~/.local/bin"
