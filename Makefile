.PHONY: proto build clean run-query run-health install-deps test mock-server

# Variables
PROTO_DIR = proto
BINARY_NAME = observo-connector
MOCK_SERVER = mock-server

# Default target
all: install-deps proto build

# Install dependencies
install-deps:
	go mod tidy
	go mod download

# Install protoc tools if needed
install-protoc:
	@echo "Installing protoc tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go code from proto files
proto:
	@echo "Generating protobuf code..."
	@export PATH=$$PATH:$$(go env GOPATH)/bin && \
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/vizierapi.proto

# Build the binary
build: proto
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) ./cmd

# Build mock server
mock-server:
	@echo "Building $(MOCK_SERVER)..."
	go build -o $(MOCK_SERVER) ./cmd/mock-server

# Build all binaries
build-all: build mock-server

# Clean generated files and binaries
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME) $(MOCK_SERVER)
	rm -f $(PROTO_DIR)/*.pb.go

# Run tests
test: build mock-server
	@echo "Running tests..."
	./test.sh

# Example commands to run queries
run-query: build
	./$(BINARY_NAME) query "df.head(5)" \
		--cluster-id=your-cluster-id \
		--address=vizier.pixie.svc.cluster.local:51400 \
		--skip-verify=true

run-health: build
	./$(BINARY_NAME) health \
		--cluster-id=your-cluster-id \
		--address=vizier.pixie.svc.cluster.local:51400 \
		--skip-verify=true

# Run with mutual TLS (when you have client certificates)
run-query-mtls: build
	./$(BINARY_NAME) query "df.head(5)" \
		--cluster-id=your-cluster-id \
		--address=vizier.pixie.svc.cluster.local:51400 \
		--ca-cert=certs/ca.crt \
		--client-cert=certs/client.crt \
		--client-key=certs/client.key \
		--server-name=vizier.pixie.svc.cluster.local

# Test with mock server
test-mock: build mock-server
	@echo "Starting mock server..."
	./$(MOCK_SERVER) & \
	SERVER_PID=$$!; \
	sleep 2; \
	echo "Testing health check..."; \
	./$(BINARY_NAME) health \
		--cluster-id=test-cluster \
		--address=localhost:51400 \
		--disable-ssl=true; \
	echo "Testing query..."; \
	./$(BINARY_NAME) query "df.head(5)" \
		--cluster-id=test-cluster \
		--address=localhost:51400 \
		--disable-ssl=true; \
	kill $$SERVER_PID

# Install protoc if not available
check-protoc:
	@which protoc > /dev/null || { \
		echo "protoc not found. Installing..."; \
		echo "Ubuntu/Debian: apt install protobuf-compiler"; \
		echo "macOS: brew install protobuf"; \
		echo "Or download from: https://github.com/protocolbuffers/protobuf/releases"; \
		exit 1; \
	}

# Setup development environment
setup: check-protoc install-protoc install-deps
	@echo "Development environment ready!"

# Create certificates directory with dummy certs (for demo purposes)
certs:
	@mkdir -p certs
	@echo "Creating dummy certificates for demo..."
	@echo "-----BEGIN CERTIFICATE-----" > certs/ca.crt
	@echo "MIIBkTCB+wIJAMlyFqk69v+9MA0GCSqGSIb3DQEBCwUAMBQxEjAQBgNVBAMMCWxv" >> certs/ca.crt
	@echo "Y2FsaG9zdDAeFw0yMzEyMDEwMDAwMDBaFw0yNCEyMzEyMzU5NTlaMBQxEjAQBgNV" >> certs/ca.crt
	@echo "BAMMCWxvY2FsaG9zdDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABHxYj5k6lN4t" >> certs/ca.crt
	@echo "-----END CERTIFICATE-----" >> certs/ca.crt
	@echo "-----BEGIN CERTIFICATE-----" > certs/client.crt
	@echo "MIIBkTCB+wIJAMlyFqk69v+9MA0GCSqGSIb3DQEBCwUAMBQxEjAQBgNVBAMMCWxv" >> certs/client.crt
	@echo "Y2FsaG9zdDAeFw0yMzEyMDEwMDAwMDBaFw0yNCEyMzEyMzU5NTlaMBQxEjAQBgNV" >> certs/client.crt
	@echo "BAMMCWxvY2FsaG9zdDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABHxYj5k6lN4t" >> certs/client.crt
	@echo "-----END CERTIFICATE-----" >> certs/client.crt
	@echo "-----BEGIN PRIVATE KEY-----" > certs/client.key
	@echo "MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg7S8j9k0Z5N8Q7y+p" >> certs/client.key
	@echo "-----END PRIVATE KEY-----" >> certs/client.key
	@echo "Demo certificates created in certs/ directory"

# Deploy to Kubernetes
deploy:
	./deploy.sh

# Show help
help:
	@echo "Available targets:"
	@echo "  all         - Install deps, generate proto, and build"
	@echo "  build       - Build the observo-connector binary"
	@echo "  mock-server - Build the mock server binary"
	@echo "  build-all   - Build both binaries"
	@echo "  proto       - Generate protobuf code"
	@echo "  test        - Run test script"
	@echo "  test-mock   - Test with mock server"
	@echo "  clean       - Clean generated files"
	@echo "  setup       - Setup development environment"
	@echo "  certs       - Create dummy certificates"
	@echo "  deploy      - Deploy to Kubernetes"
	@echo "  help        - Show this help"
