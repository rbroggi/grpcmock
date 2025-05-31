# Debugging Your Go `protoc` Plugin by Capturing the CodeGeneratorRequest

This guide explains how to capture the `CodeGeneratorRequest` that `protoc` (or `buf` acting as a `protoc` driver) sends to your Go plugin. By saving this request to a file, you can then use it as direct input to debug your plugin in GoLand IDE, allowing you to see the exact data your plugin receives.

## Understanding How `protoc` Plugins Work

`protoc` (the Protocol Buffer compiler) communicates with plugins using a simple stdin/stdout contract:

1.  **Input (`CodeGeneratorRequest`)**: `protoc` serializes a `pluginpb.CodeGeneratorRequest` message. This message contains:
    * All the parsed `.proto` files the plugin needs to process (`proto_file` field).
    * The specific files `protoc` wants the plugin to generate code for (`file_to_generate` field).
    * Any parameters passed to the plugin via the `--myplugin_opt` flag or `opt` in `buf.gen.yaml` (`parameter` field).
    * Compiler version information.
      This serialized message is written to the plugin's standard input (stdin).

2.  **Processing**: The plugin reads the `CodeGeneratorRequest` from its stdin, unmarshals it, and performs its code generation logic based on the request's content.

3.  **Output (`CodeGeneratorResponse`)**: The plugin then serializes a `pluginpb.CodeGeneratorResponse` message. This message contains:
    * The generated output files as a list of `pluginpb.CodeGeneratorResponse.File` messages (name, content, etc.).
    * An error message if processing failed.
    * Information about features supported by the plugin (e.g., `FEATURE_PROTO3_OPTIONAL`).
      This serialized response is written to the plugin's standard output (stdout).

4.  **`protoc` Action**: `protoc` reads the `CodeGeneratorResponse` from the plugin's stdout and writes the generated files to disk or reports errors.

Our goal in this debugging approach is to intercept and save the `CodeGeneratorRequest` (Step 1) before your actual plugin processes it.

## Overview of Debugging Approaches (`protoc` vs. `buf`)

Both `protoc` and `buf` can be used to drive plugin execution. The core idea of using a wrapper script/binary to capture the input remains the same, but how you instruct the tool to use this wrapper differs:

* **Direct `protoc`**:
  You use the `--plugin=protoc-gen-myplugin=/path/to/wrapper-script` flag. This tells `protoc` that for the plugin named `myplugin`, it should execute `/path/to/wrapper-script`. The wrapper script then saves the stdin.

* **`buf` CLI**:
  `buf` typically uses a `buf.gen.yaml` configuration file to define plugins, their options, and output locations. To use the wrapper, you modify this YAML file (or a dedicated debug version of it) to point the `plugin` directive for the plugin you're debugging to your wrapper script/binary (e.g., `protoc-gen-saverequest`). `buf` then invokes this wrapper when you run `buf generate`. `buf` handles finding the plugin executable, often relying on the system `PATH`.

This guide will now focus on the `buf` approach using your `protoc-gen-saverequest` utility, as demonstrated in the [`examples/company_services`](../examples/company_services) folder.

## Method: Capturing Request with `protoc-gen-saverequest` and `buf`

This method uses the utility `protoc-gen-saverequest` and a specific `buf.gen.debug.yaml` to capture the input for your actual Go plugin. See the working example in [`examples/company_services`](../examples/company_services).

### Prerequisites

* You are using `buf` to generate code from your `.proto` files.
* Your Go plugin project is set up in GoLand.
* You have a directory (e.g., your project root or a dedicated `proto` directory) from which you run `buf generate`.
* The [`examples/company_services`](../examples/company_services) folder contains:
  - `protoc-gen-saverequest` (the capture script)
  - `buf.gen.debug.yaml` (debug plugin config)
  - `buf.gen.yaml` (normal plugin config)

### Step 1: Use the Provided `protoc-gen-saverequest` Utility

This utility acts as a temporary plugin. Its sole purpose is to capture whatever `buf` (via `protoc`) sends to its stdin and save it to a file. The name `protoc-gen-saverequest` follows the standard naming convention for `protoc` plugins, making it easy for `buf` to discover if it's in the `PATH`.

You can find an example script at [`examples/company_services/protoc-gen-saverequest`](../examples/company_services/protoc-gen-saverequest):

```bash
#!/bin/bash
# This script captures stdin and saves it to a file.
# The path ./request.bin is an example; choose a suitable path.

cat > ./request.bin
# The exit status of this script can matter to protoc.
# Exit with 0 if you want protoc to think the "plugin" succeeded.
# If you want to see protoc errors, you might exit with a non-zero code.
exit 0
```

Make the utility executable:
```bash
chmod +x examples/company_services/protoc-gen-saverequest
```

This utility will output the captured `CodeGeneratorRequest` to `./request.bin` in the `examples/company_services` directory.

### Step 2: Use the Provided `buf.gen.debug.yaml` File

This file instructs `buf` to use your `protoc-gen-saverequest` utility for a specific plugin invocation. See [`examples/company_services/buf.gen.debug.yaml`](../examples/company_services/buf.gen.debug.yaml):

```yaml
version: v1
plugins:
  # Standard Go and gRPC code generation
  - plugin: buf.build/protocolbuffers/go:v1.31.0
    out: protos
    opt:
      - paths=source_relative
  - plugin: buf.build/grpc/go:v1.3.0
    out: protos
    opt:
      - paths=source_relative
      - require_unimplemented_servers=true

  # Your grpcmock plugin (debug capture)
  - plugin: saverequest
    strategy: all
    out: protos
    opt:
      - http_port=9090
      - grpc_port=9001
      - package_name=main
      # - output_filename=my_mock_server.go # Optional: to change from default grpcmockserver.go
```

* **`plugin: saverequest`**: When `buf` processes this, it will look for an executable named `protoc-gen-saverequest` in the `PATH`.
* **`opt: ...`**: These options (`http_port`, `grpc_port`, `package_name`, etc.) are the same as those used for your actual `grpcmock` plugin (see below).

### Step 3: Run `buf generate` with the Debug Template

From the `examples/company_services` directory, run:

    ```bash
    PATH=$PATH:$(pwd) buf generate . --template=./buf.gen.debug.yaml
    ```
    * `PATH=$PATH:$(pwd)`: This temporarily adds the current working directory (`pwd`) to your system's `PATH` for this command's execution. This ensures that `buf` can find your `protoc-gen-saverequest` executable if you placed it in the current directory.
    * `buf generate .`: Tells `buf` to generate code for the Protobuf sources found in the current directory (or as defined by `buf.yaml`'s `roots`).
    * `--template=./buf.gen.debug.yaml`: Instructs `buf` to use your custom debug generation template.

After this command runs, the file `./request.bin` will contain the serialized `CodeGeneratorRequest` that was sent to `protoc-gen-saverequest`.

### Step 4: Set Up GoLand Debug Configuration

This step configures GoLand to run your *actual* Go plugin, using the captured `./request.bin` as its standard input.

1.  Open your Go plugin project in GoLand.
2.  Go to **Run** -> **Edit Configurations...**.
3.  Click the **+** (Add New Configuration) button and select **Go Build**.
4.  Configure the settings:
    * **Name:** A descriptive name, e.g., "Debug MyActualPlugin with Saved Request".
    * **Run kind:** Choose "Package" (and select your `main` package) or "File" (and select the `main.go` file of your *actual* plugin).
    * **Output directory:** (Optional) GoLand usually handles this.
    * **Program arguments:** Generally, leave this empty. The arguments/options for your plugin are already embedded in the `CodeGeneratorRequest.parameter` field within `./request.bin` (thanks to the `opt` section in `buf.gen.debug.yaml`).
    * **Environment:** Set any necessary environment variables your plugin might require during its execution.
    * **Redirect input from:**
        * Check the box "Redirect input from".
        * Click the folder icon and select the `./request.bin` file that was generated in Step 3.

### Step 5: Debug Your Actual Plugin in GoLand

Now you're ready to debug your real Go plugin.

1.  Open the Go source file of your *actual* plugin where you process the `CodeGeneratorRequest` (typically in your `main` function or a function called by it).
2.  Ensure your code reads from `os.Stdin` and unmarshals the `pluginpb.CodeGeneratorRequest`. Example snippet:
    ```go
    package main

    import (
    	"io"
    	"log"
    	"os"

    	"google.golang.org/protobuf/proto"
    	"google.golang.org/protobuf/types/pluginpb"
    	// Your other plugin-specific imports
    )

    func main() {
    	// Read the CodeGeneratorRequest from stdin (which will be /tmp/request.bin)
    	inputBytes, err := io.ReadAll(os.Stdin)
    	if err != nil {
    		log.Fatalf("FATAL: Failed to read stdin: %v", err)
    	}

    	// Unmarshal the request
    	req := &pluginpb.CodeGeneratorRequest{}
    	if err := proto.Unmarshal(inputBytes, req); err != nil {
    		log.Fatalf("FATAL: Failed to unmarshal CodeGeneratorRequest: %v", err)
    	}

    	// <<< SET YOUR BREAKPOINT HERE (or anywhere after 'req' is populated) >>>
    	log.Printf("Successfully unmarshaled request. Files to generate: %d", len(req.FileToGenerate))
    	log.Printf("Parameter from protoc/buf: '%s'", req.GetParameter())

    	// ...
    	// Your actual plugin logic starts here, using the 'req' object.
    	// For example, iterate through req.FileToGenerate and req.ProtoFile,
    	// and use req.Parameter to configure your plugin's behavior.
    	// ...

    	// Example: creating and sending a response (your plugin will do this)
    	// response := &pluginpb.CodeGeneratorResponse{}
    	// // Populate response.File, response.Error, response.SupportedFeatures
    	//
    	// outputBytes, err := proto.Marshal(response)
    	// if err != nil {
    	//  log.Fatalf("FATAL: Failed to marshal CodeGeneratorResponse: %v", err)
    	// }
    	// if _, err := os.Stdout.Write(outputBytes); err != nil {
    	//  log.Fatalf("FATAL: Failed to write response to stdout: %v", err)
    	// }
    	// log.Println("Plugin finished successfully.")
    }
    ```
3.  Set breakpoints in your Go code where you want to inspect the `req` object or step through your plugin's logic. A good initial breakpoint is right after `proto.Unmarshal`.
4.  Select the debug configuration you created in Step 4 from the dropdown in the GoLand toolbar.
5.  Click the **Debug** icon (the bug symbol 🐞).

GoLand will build and run your *actual* Go plugin, feeding it the content of `./request.bin` as its standard input. The debugger will stop at your breakpoints, allowing you to inspect the `req` variable in detail (files, parameters, etc.) and step through your plugin's execution flow with the exact input `buf` provided.

## Reference: Your Actual Plugin Configuration

For comparison, your normal plugin configuration in [`examples/company_services/buf.gen.yaml`](../examples/company_services/buf.gen.yaml) looks like:

```yaml
version: v1
plugins:
  # Standard Go and gRPC code generation
  - plugin: buf.build/protocolbuffers/go:v1.31.0
    out: protos
    opt:
      - paths=source_relative
  - plugin: buf.build/grpc/go:v1.3.0
    out: protos
    opt:
      - paths=source_relative
      - require_unimplemented_servers=true

  # Your grpcmock plugin
  - plugin: grpcmock
    strategy: all
    out: protos
    opt:
      - http_port=9090
      - grpc_port=9001
      - package_name=main
      # - output_filename=my_mock_server.go # Optional: to change from default grpcmockserver.go
```

Make sure the `opt` section in your debug config matches the arguments and parameters you use for your internal plugin.
