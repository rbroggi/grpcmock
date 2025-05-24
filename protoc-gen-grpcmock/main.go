package main

import (
	"flag"
	"io"
	"log"
	"os"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

func main() {
	os.Exit(mainLogic())
}

func mainLogic() int {
	var flags flag.FlagSet
	httpPort := flags.String("http_port", "8081", "Default HTTP port for the mock server")
	grpcPort := flags.String("grpc_port", "4770", "Default gRPC port for the mock server")
	// This output_filename will be the name of the single generated server file.
	outputFilename := flags.String("output_filename", "grpcmockserver.go", "Name of the single generated mock server file")
	// This packageName will be the package of the single generated server file (e.g., "main").
	packageName := flags.String("package_name", "main", "Go package name for the generated server file")
	// Module path for importing the runtime, ensure this matches your project's module path.
	// It's now taken from a const in generator.go but could be an option if more flexibility is needed.
	// pluginModulePath := flags.String("module_path", "github.com/rbroggi/grpcmock", "Go module path of the grpcmock project for runtime import")

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Printf("grpcmock: failed to read input: %v", err)
		return 1
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(input, req); err != nil {
		log.Printf("grpcmock: failed to unmarshal CodeGeneratorRequest: %v", err)
		return 1
	}

	optsMap := make(map[string]string)
	if req.Parameter != nil {
		params := strings.Split(req.GetParameter(), ",")
		for _, param := range params {
			parts := strings.SplitN(param, "=", 2)
			if len(parts) == 2 {
				optsMap[parts[0]] = parts[1]
			}
		}
	}

	if port, ok := optsMap["http_port"]; ok {
		*httpPort = port
	}
	if port, ok := optsMap["grpc_port"]; ok {
		*grpcPort = port
	}
	if name, ok := optsMap["output_filename"]; ok {
		*outputFilename = name
	}
	if pkg, ok := optsMap["package_name"]; ok {
		*packageName = pkg
	}
	// if modPath, ok := optsMap["module_path"]; ok {
	// 	*pluginModulePath = modPath
	// }

	opts := protogen.Options{
		ParamFunc: flags.Set,
	}

	plugin, err := opts.New(req)
	if err != nil {
		log.Printf("grpcmock: failed to create plugin: %v", err)
		return 1
	}

	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	// Call generateMockServer ONCE with the plugin object, which contains all files.
	if err := generateMockServer(plugin, *outputFilename, *packageName, *httpPort, *grpcPort); err != nil {
		// plugin.Error sets the error in the response to protoc
		plugin.Error(err)
		// also log it for plugin's own stderr trace
		log.Printf("grpcmock: error generating mock server: %v", err)
	}

	resp := plugin.Response()
	out, err := proto.Marshal(resp)
	if err != nil {
		log.Printf("grpcmock: failed to marshal response: %v", err)
		return 1
	}

	if _, err := os.Stdout.Write(out); err != nil {
		log.Printf("grpcmock: failed to write response: %v", err)
		return 1
	}
	return 0
}
