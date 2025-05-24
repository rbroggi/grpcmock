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

// Config holds all generator options for clarity and maintainability.
type Config struct {
	httpPort       string
	grpcPort       string
	outputFilename string
	packageName    string
}

// parseConfig parses flags and request parameters into a Config struct.
func parseConfig(req *pluginpb.CodeGeneratorRequest) Config {
	flags := flag.NewFlagSet("grpcmock", flag.ContinueOnError)
	cfg := Config{
		httpPort:       "8081",
		grpcPort:       "4770",
		outputFilename: "grpcmockserver.go",
		packageName:    "main",
	}
	flags.StringVar(&cfg.httpPort, "http_port", cfg.httpPort, "Default HTTP port for the mock server")
	flags.StringVar(&cfg.grpcPort, "grpc_port", cfg.grpcPort, "Default gRPC port for the mock server")
	flags.StringVar(&cfg.outputFilename, "output_filename", cfg.outputFilename, "Name of the single generated mock server file")
	flags.StringVar(&cfg.packageName, "package_name", cfg.packageName, "Go package name for the generated server file")

	// Parse parameters from protoc request
	if req != nil && req.Parameter != nil {
		params := strings.Split(req.GetParameter(), ",")
		for _, param := range params {
			parts := strings.SplitN(param, "=", 2)
			if len(parts) == 2 {
				switch parts[0] {
				case "http_port":
					cfg.httpPort = parts[1]
				case "grpc_port":
					cfg.grpcPort = parts[1]
				case "output_filename":
					cfg.outputFilename = parts[1]
				case "package_name":
					cfg.packageName = parts[1]
				}
			}
		}
	}
	return cfg
}

// logAndReturn logs an error and returns the given code.
func logAndReturn(msg string, err error, code int) int {
	log.Printf(msg, err)
	return code
}

func main() {
	os.Exit(mainLogic())
}

func mainLogic() int {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return logAndReturn("grpcmock: failed to read input: %v", err, 1)
	}

	req := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(input, req); err != nil {
		return logAndReturn("grpcmock: failed to unmarshal CodeGeneratorRequest: %v", err, 1)
	}

	cfg := parseConfig(req)

	opts := protogen.Options{
		ParamFunc: nil, // Do not use flag.CommandLine.Set; we handle params ourselves
	}

	plugin, err := opts.New(req)
	if err != nil {
		return logAndReturn("grpcmock: failed to create plugin: %v", err, 1)
	}

	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	if err := generateMockServer(plugin, cfg.outputFilename, cfg.packageName, cfg.httpPort, cfg.grpcPort); err != nil {
		plugin.Error(err)
		log.Printf("grpcmock: error generating mock server: %v", err)
	}

	resp := plugin.Response()
	out, err := proto.Marshal(resp)
	if err != nil {
		return logAndReturn("grpcmock: failed to marshal response: %v", err, 1)
	}

	if _, err := os.Stdout.Write(out); err != nil {
		return logAndReturn("grpcmock: failed to write response: %v", err, 1)
	}
	return 0
}
