# GRPCMock: gRPC Mock Server Generator

GRPCMock is a `protoc` plugin written in Go that generates a runnable gRPC mock server based on your `.proto` service definitions. The generated server allows you to mock gRPC API responses and errors, and provides HTTP endpoints to set expectations and verify interactions, making it ideal for component and integration testing of your gRPC clients.

This project is inspired by tools like [MockServer](https://www.mock-server.com/) (for HTTP) and takes cues from projects like `gripmock`, but with a primary focus on **generating a self-contained, runnable Go mock server** that is compliant with [Buf](https://buf.build/).

## Features

* **Protoc Plugin**: Integrates directly into your protobuf compilation toolchain.
* **Go-based Mock Server**: Generates a `server.go` file that implements all specified gRPC services.
* **HTTP Control Plane**:
    * Manage expectations via HTTP:
        * `POST /expectations`: Add a new expectation.
        * `GET /expectations`: List all current expectations.
        * `DELETE /expectations`: Clear all expectations and recorded calls.
    * Verify calls via HTTP:
        * `GET /verifications`: List all gRPC calls received by the mock server.
* **Request Matching**: Define expectations based on:
    * gRPC method name.
    * Request headers (supports regex matching for header values).
    * Request body fields (JSON representation, exact match).
* **Response Mocking**: Configure mock server to return:
    * Specific protobuf message responses (defined as JSON).
    * Custom gRPC status codes and error messages.
    * Custom response headers.
* **Buf Compatible**: Designed to work seamlessly with Buf's code generation workflows.
* **Standalone Server**: The generated `server.go` can be run as an executable.

## Project Structure

* `protoc-gen-grpcmock/`: Contains the source code for the `protoc` plugin.
    * `main.go`: Entry point for the plugin.
    * `generator.go`: Core logic for parsing protobuf definitions and applying templates.
    * `server.tmpl`: Go template used to generate the `server.go` mock server.
    * `runtime/`: A Go package containing the shared runtime logic for the generated mock server (HTTP handlers, expectation storage, matching logic, etc.). This allows for easier development and testing of the core mocking functionality.
* `examples/`: Contains example `.proto` files and Buf configurations to demonstrate usage.
* `go.mod`, `go.sum`: Go module files for the plugin project.
* `buf.gen.yaml`: (Optional, in root) Can be used for developing the plugin itself against examples.
* `Makefile`: Provides helper commands for building, installing, and testing.

## Usage

### Prerequisites

* Go (version 1.21+ recommended)
* [Buf CLI](https://buf.build/docs/installation)
* Protobuf compiler (`protoc`) (usually managed by Buf, but good to have for understanding)

### Install the Plugin

Build and install the `protoc-gen-grpcmock` plugin to your `$GOBIN` path (ensure `$GOBIN` is in your system `PATH`):

```bash
make install
# Or manually:
# go install ./protoc-gen-grpcmock
```

Replace `grpcmock` in `protoc-gen-grpcmock/generator.go`'s `currentPluginModulePath` constant with your actual Go module path if it's different (e.g., `github.com/yourusername/grpcmock`). 

### Define Your Protobuf Services

Create your `.proto` files as usual. See the `examples/company_services/` directory for an example.

### Configure Buf for Generation

In the directory containing your `.proto` files (or a parent directory), create/update your `buf.gen.yaml`:

```yaml
# Example: my-project/buf.gen.yaml
version: v1
plugins:
  # Standard Go and gRPC code generation
  - plugin: buf.build/protocolbuffers/go:v1.31.0
    out: gen/go
    opt:
      - paths=source_relative
  - plugin: buf.build/grpc/go:v1.3.0
    out: gen/go
    opt:
      - paths=source_relative
      - require_unimplemented_servers=true

  # GRPCMock plugin
  - plugin: grpcmock # Assumes protoc-gen-grpcmock is in your PATH
    # path: /path/to/protoc-gen-grpcmock # If not in PATH
    out: gen/grpcmock # Output directory for the mock server
    opt:
      - http_port=9090    # Default HTTP port for mock control
      - grpc_port=9001    # Default gRPC port for the mock server
      # - module_path=github.com/your/module # If your plugin needs to know its own module path
      # and it's different from the default in generator.go
```

The `out` path for `grpcmock` will determine where the generated `server.go` (and its package structure) is placed.

### Generate Code

Navigate to your project directory (where `buf.yaml` is) and run:

```bash
buf generate
```

This will generate the standard Go gRPC stubs and also your `server.go` mock server in the specified `out` directories.

### Run the Mock Server

The generated mock server (e.g., `gen/grpcmock/your_proto_package/server.go`) includes a `main` function. You can run it directly:

```bash
go run ./gen/grpcmock/your_proto_package/v1/server.go \
--grpc-port=9001 \
--http-port=9090
```
(Adjust the path and ports as per your setup.)

You can also override ports with environment variables: `GRPCMOCK_GRPC_PORT` and `GRPCMOCK_HTTP_PORT`.

### Interact with the Mock Server

1. Setting Expectations (HTTP)
    Send a `POST` request to the `/expectations` endpoint (e.g., `http://localhost:9090/expectations`).
    Example Expectation JSON:
    ```json5
    {
      "fullMethodName": "/company_services.employee.v1.EmployeeService/GetDetails",
      "requestMatcher": {
        "body": {
          "employee_id": "emp-123"
        },
        "headers": {
          "x-tenant-id": "^(tenant-a|tenant-b)$"
        }
      },
      "response": {
        "body": {
          // JSON representation of the EmployeeResponse proto
          "employee": {
            "id": "emp-123",
            "name": "Mocked Employee",
            "position": "Senior Mock Specialist",
            "department_id": "dept-dev"
          }
        }
        // "headers": { // Optional response headers
        //   "x-mock-hit": "true"
        // },
        // "error": { // To return a gRPC error instead
        //   "code": 5, // gRPC status code (e.g., 5 for NOT_FOUND)
        //   "message": "Employee not found"
        // }
      }
    }
    ```
    Using curl:
    ```bash
    curl -X POST http://localhost:9090/expectations -d '{...}' # (paste JSON above)
    ```
2. Listing Expectations (HTTP)
    ```bash
    curl http://localhost:9090/expectations
    ```
3. Clearing Expectations (HTTP)
    ```bash
    curl -X DELETE http://localhost:9090/expectations
    ```
4. Making gRPC Calls to the Mock
    Your gRPC client application can now connect to the mock gRPC server (e.g., `localhost:9001`). Calls matching an expectation will receive the mocked response/error. Calls not matching any expectation will typically receive a gRPC `Unimplemented` error.
5. Verifying Calls (HTTP)
    Retrieve a list of all calls made to the mock server:
```bash
curl http://localhost:9090/verifications
```
This returns a JSON array of RecordedGRPCCall objects.

## Development Lifecycle
The `grpcmock` project itself (the `protoc-gen-grpcmock` plugin and its `runtime` package) can be developed like any Go project.

* `protoc-gen-grpcmock/runtime/`: This package contains the core, non-generated logic (HTTP handlers, storage, matching). You can modify and test this package independently. Changes here don't require re-templating unless the interface with the generated code changes. This allows for faster iteration on the HTTP API, matching features, etc., with the benefits of Go's type checking and testing.
* `protoc-gen-grpcmock/server.tmpl`: Modify this template if you need to change the structure of the generated gRPC method stubs or how they integrate with the runtime.
* `protoc-gen-grpcmock/generator.go`: Update this if you change the template data structure or the logic for extracting information from protos.

### Building the Plugin
```bash
make build
# or
# go build -o protoc-gen-grpcmock ./protoc-gen-grpcmock
```

### Testing with Examples
1. Modify the plugin or runtime.
2. Rebuild/reinstall the plugin: `make install`.
3. Navigate to an example directory: `cd examples/company_services`.
4. Generate the mock server for the example: `buf generate` (or `make generate-example` from root).
5. Run the generated server: `go run ./out/grpcmock/company_services/server.go` (or `make run-example-server`).
6. Write test clients (gRPC and HTTP) to interact with the running mock server.

### Future Enhancements
* More sophisticated request body matchers (contains, regex per field, JSONPath, ignoring extra fields).
* Advanced expectation conditions (e.g., call count, call order).
* Detailed stream expectation support.
* Web UI for managing expectations and verifications.
* Persistence layer for expectations.

Contributions are welcome!