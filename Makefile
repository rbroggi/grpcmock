.PHONY: all build install clean lint generate-example run-example-customer-server run-example-employee-server help

# Variables
PLUGIN_NAME := grpcmock
PLUGIN_DIR := ./protoc-gen-grpcmock
PLUGIN_OUTPUT_NAME := protoc-gen-$(PLUGIN_NAME)
GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
    GOBIN := $(shell go env GOPATH)/bin
endif
PLUGIN_INSTALL_PATH := $(GOBIN)/$(PLUGIN_OUTPUT_NAME)

# Define Go module path (IMPORTANT: Update if your module path is different)
# This should match the 'currentPluginModulePath' in generator.go and your project's go.mod
# For the provided project structure, the root go.mod is "module grpcmock" [cite: 1]
PLUGIN_MODULE_PATH := grpcmock


EXAMPLE_DIR := ./examples/company_services
EXAMPLE_OUT_DIR := $(EXAMPLE_DIR)/out
# Adjusted path based on how generator.go (source 37-40) constructs output paths
# relative to the plugin's module path and the input proto's path.
# If PLUGIN_MODULE_PATH is "grpcmock", and protos are in "examples/company_services/customer/v1",
# the output path inside `out/grpcmock/` would be `grpcmock/examples/company_services/customer/v1/server.go`.
EXAMPLE_GEN_MOCK_ROOT_DIR := $(EXAMPLE_OUT_DIR)/grpcmock
EXAMPLE_CUSTOMER_SERVER_PATH := $(EXAMPLE_GEN_MOCK_ROOT_DIR)/$(PLUGIN_MODULE_PATH)/examples/company_services/customer/v1/server.go
EXAMPLE_EMPLOYEE_SERVER_PATH := $(EXAMPLE_GEN_MOCK_ROOT_DIR)/$(PLUGIN_MODULE_PATH)/examples/company_services/employee/v1/server.go


# Default target
all: build

# Build the protoc plugin
build:
	@echo "Building $(PLUGIN_OUTPUT_NAME) plugin..."
	@go build -o $(PLUGIN_OUTPUT_NAME) $(PLUGIN_DIR)
	@echo "$(PLUGIN_OUTPUT_NAME) built successfully."

# Install the protoc plugin to GOBIN
install: build
	@echo "Installing $(PLUGIN_OUTPUT_NAME) to $(PLUGIN_INSTALL_PATH)..."
	@mkdir -p $(GOBIN)
	@cp $(PLUGIN_OUTPUT_NAME) $(PLUGIN_INSTALL_PATH)
	@echo "$(PLUGIN_OUTPUT_NAME) installed to $(PLUGIN_INSTALL_PATH)."
	@echo "Please ensure $(GOBIN) is in your PATH."

# Clean build artifacts and generated example files
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(PLUGIN_OUTPUT_NAME)
	@echo "Cleaning generated example files from $(EXAMPLE_OUT_DIR)..."
	@rm -rf $(EXAMPLE_OUT_DIR)
	@echo "Clean complete."

# Run Go linters (requires golangci-lint or similar)
# You might need to run `go mod tidy` in relevant directories first.
lint:
	@echo "Running linter (ensure golangci-lint is installed)..."
	@(cd $(PLUGIN_DIR)/runtime && go mod tidy)
	@(cd $(PLUGIN_DIR) && go mod tidy)
	@golangci-lint run ./... || echo "Linter found issues or is not installed."

# Generate code for the company_services example using Buf
generate-example:
	@echo "Generating code for example: $(EXAMPLE_DIR)..."
	@(cd $(EXAMPLE_DIR) && buf generate)
	@echo "Example code generation complete. Output in $(EXAMPLE_OUT_DIR)"

run-example-customer-server: generate-example
	@echo "Running generated mock server for Customer service..."
	@echo "Command: go run $(EXAMPLE_CUSTOMER_SERVER_PATH) --http-port=9090 --grpc-port=9001"
	@go run $(EXAMPLE_CUSTOMER_SERVER_PATH) --http-port=9090 --grpc-port=9001

run-example-employee-server: generate-example
	@echo "Running generated mock server for Employee service..."
	@echo "Command: go run $(EXAMPLE_EMPLOYEE_SERVER_PATH) --http-port=9092 --grpc-port=9003"
	@go run $(EXAMPLE_EMPLOYEE_SERVER_PATH) --http-port=9092 --grpc-port=9003

# Placeholder for running actual integration tests against the example
test-example: generate-example
	@echo "Testing example (placeholder)..."
	@echo "Start the example server(s) first, then run your client tests."
	# e.g., (cd examples/company_services && go test ./...) # (You'd need actual test files)

# Show help message
help:
	@echo "Available targets:"
	@echo "  all                         - Build the project (default)"
	@echo "  build                       - Build the $(PLUGIN_OUTPUT_NAME) plugin"
	@echo "  install                     - Install the plugin to \$$GOBIN"
	@echo "  clean                       - Remove build artifacts and generated example files"
	@echo "  lint                        - Run Go linters (e.g., golangci-lint)"
	@echo "  generate-example            - Generate code for the company_services example using Buf"
	@echo "  run-example-customer-server - Run generated Customer service mock (HTTP:9090, gRPC:9001)"
	@echo "  run-example-employee-server - Run generated Employee service mock (HTTP:9092, gRPC:9003)"
	@echo "  test-example                - Placeholder for running integration tests for the example"
	@echo "  help                        - Show this help message"
