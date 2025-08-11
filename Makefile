.PHONY: proto build clean run-query run-health run-server install-deps test install uninstall fmt lint check setup-certs help dev release

# Variables
PROTO_DIR = proto
BINARY_NAME = observo-connector
SERVER_PORT = 8080

# Default target
all: install-deps proto build

# Install dependencies
install-deps:
	go mod tidy
	go mod download

# Generate Go code from proto files
proto:
	@echo "Generating protobuf code..."
	@export PATH=$$PATH:$$(go env GOPATH)/bin && \
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/vizierapi_simple.proto

# Build the binary
build: proto
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) ./cmd

# Clean generated files and binaries
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(PROTO_DIR)/*.pb.go

# Run tests
test: build
	@echo "Running tests..."
	go test ./... -v

# Run query command (example)
run-query: build
	@echo "Running example query..."
	./$(BINARY_NAME) query "dx.display_name" \
		--cluster-id=demo-cluster \
		--address=localhost:50051 \
		--disable-ssl

# Run health check
run-health: build
	@echo "Running health check..."
	./$(BINARY_NAME) health \
		--cluster-id=demo-cluster \
		--address=localhost:50051 \
		--disable-ssl

# Start HTTP server
run-server: build
	@echo "Starting HTTP server on port $(SERVER_PORT)..."
	./$(BINARY_NAME) server \
		--port=$(SERVER_PORT) \
		--cluster-id=demo-cluster \
		--address=localhost:50051 \
		--disable-ssl

# Install observo-connector binary to system
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "✅ $(BINARY_NAME) installed successfully"

# Create certificates directory
setup-certs:
	@echo "Setting up certificates directory..."
	mkdir -p certs
	@echo "✅ Certificates directory created"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Show help
help:
	@echo "Available commands:"
	@echo "  make build         - Build the observo-connector binary"
	@echo "  make proto         - Generate protobuf code"
	@echo "  make test          - Run tests"
	@echo "  make clean         - Clean generated files"
	@echo "  make run-query     - Run example query"
	@echo "  make run-health    - Run health check"
	@echo "  make run-server    - Start HTTP server"
	@echo "  make setup-certs   - Create certificates directory"
	@echo "  make fmt           - Format code"
	@echo "  make help          - Show this help"
