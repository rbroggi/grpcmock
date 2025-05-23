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
	// Plugin options can be added here via flags if needed.
	// For example, the default HTTP port for the mock server.
	var (
		flags    flag.FlagSet
		httpPort = flags.String("http_port", "8081", "Default HTTP port for the mock server")
		grpcPort = flags.String("grpc_port", "4770", "Default gRPC port for the mock server")
	)

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("grpcmock: failed to read input: %v", err)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(input, req); err != nil {
		log.Fatalf("grpcmock: failed to unmarshal CodeGeneratorRequest: %v", err)
	}

	// Parse parameters
	// Example: "http_port=9090,grpc_port=9000"
	// We will prioritize parameters passed via protoc's --<plugin_name>_opt flag
	// Over flags defined in the plugin itself for better Buf integration.
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

	// Override default flag values if present in protoc options
	if port, ok := optsMap["http_port"]; ok {
		*httpPort = port
	}
	if port, ok := optsMap["grpc_port"]; ok {
		*grpcPort = port
	}

	opts := protogen.Options{
		ParamFunc: flags.Set,
	}

	plugin, err := opts.New(req)
	if err != nil {
		log.Fatalf("grpcmock: failed to create plugin: %v", err)
	}

	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	for _, f := range plugin.Files {
		if !f.Generate {
			continue
		}
		generateFile(plugin, f, *httpPort, *grpcPort)
	}

	resp := plugin.Response()
	out, err := proto.Marshal(resp)
	if err != nil {
		log.Fatalf("grpcmock: failed to marshal response: %v", err)
	}

	if _, err := os.Stdout.Write(out); err != nil {
		log.Fatalf("grpcmock: failed to write response: %v", err)
	}
}
