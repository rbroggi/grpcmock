package main

import (
	_ "embed"
	"fmt"
	"log"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

// TemplateData holds all data passed to the server template for code generation.
type TemplateData struct {
	Filename                  string        // Name of the generated file
	PackageName               string        // Go package name for the generated file
	Services                  []ServiceData // All services to mock
	HTTPPort                  string        // HTTP port for the mock server
	GRPCPort                  string        // gRPC port for the mock server
	HasClientStreamingMethods bool          // True if any service has client streaming methods
}

// ServiceData holds information about a single gRPC service for code generation.
type ServiceData struct {
	OriginalGoName                   string       // Original Go service name, e.g., "CustomerService"
	MockServerStructName             string       // Unique mock struct name, e.g., "CustomerServiceMockServer" or "CustomerServiceMockServer2"
	QualifiedUnimplementedServerType string       // Fully qualified UnimplementedServer type
	QualifiedRegisterServerFuncName  string       // Fully qualified RegisterServer function
	Methods                          []MethodData // Methods of the service
}

// MethodData holds information about a single gRPC method for code generation.
type MethodData struct {
	Name                      string // Original method name
	GoName                    string // Go method name
	InputType                 string // Fully qualified input type
	OutputType                string // Fully qualified output type
	ClientStreaming           bool   // True if client streaming
	ServerStreaming           bool   // True if server streaming
	FullMethodName            string // Full gRPC method name
	QualifiedStreamServerType string // Fully qualified stream server type (if streaming)
}

//go:embed server.tmpl
var serverTemplateContent string

// pendingService is a helper struct for the first pass of service collection.
type pendingService struct {
	file    *protogen.File
	service *protogen.Service
}

// countServiceNames counts occurrences of each service Go name across all files.
func countServiceNames(files []*protogen.File) map[string]int {
	counts := make(map[string]int)
	for _, file := range files {
		if !file.Generate || len(file.Services) == 0 {
			continue
		}
		for _, service := range file.Services {
			counts[service.GoName]++
		}
	}
	return counts
}

// collectPendingServices collects all services to be processed.
func collectPendingServices(files []*protogen.File) []pendingService {
	var pending []pendingService
	for _, file := range files {
		if !file.Generate || len(file.Services) == 0 {
			continue
		}
		for _, service := range file.Services {
			pending = append(pending, pendingService{file: file, service: service})
		}
	}
	return pending
}

// hasClientStreaming checks if any method in the services is client streaming.
func hasClientStreaming(services []ServiceData) bool {
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.ClientStreaming {
				return true
			}
		}
	}
	return false
}

func generateMockServer(
	gen *protogen.Plugin,
	outputFilename, targetPackageName, httpPort, grpcPort string,
) error {
	if targetPackageName == "" {
		targetPackageName = "main"
	}
	g := gen.NewGeneratedFile(outputFilename, protogen.GoImportPath(targetPackageName))

	serviceGoNameCounts := countServiceNames(gen.Files)
	pendingServices := collectPendingServices(gen.Files)
	if len(pendingServices) == 0 {
		log.Println("grpcmock: No services found in .proto files to generate a mock server.")
		return nil
	}

	// Tracks how many times a base name has been used for MockServerStructName
	serviceFinalNameTracker := make(map[string]int)
	allServices := []ServiceData{}

	for _, ps := range pendingServices {
		file := ps.file
		service := ps.service
		originalGoName := service.GoName

		currentCount := serviceFinalNameTracker[originalGoName] + 1
		serviceFinalNameTracker[originalGoName] = currentCount

		mockServerStructName := originalGoName + "MockServer"
		if serviceGoNameCounts[originalGoName] > 1 {
			mockServerStructName = fmt.Sprintf("%s%d", mockServerStructName, currentCount)
		}

		unimplementedServerTypeIdent := protogen.GoIdent{
			GoName:       "Unimplemented" + originalGoName + "Server",
			GoImportPath: file.GoImportPath,
		}
		registerServerFuncIdent := protogen.GoIdent{
			GoName:       "Register" + originalGoName + "Server",
			GoImportPath: file.GoImportPath,
		}

		svcData := ServiceData{
			OriginalGoName:                   originalGoName,
			MockServerStructName:             mockServerStructName,
			QualifiedUnimplementedServerType: g.QualifiedGoIdent(unimplementedServerTypeIdent),
			QualifiedRegisterServerFuncName:  g.QualifiedGoIdent(registerServerFuncIdent),
		}

		for _, method := range service.Methods {
			fullMethodName := fmt.Sprintf("/%s.%s/%s", file.Desc.Package(), service.Desc.Name(), method.Desc.Name())

			var qualifiedStreamServerType string
			if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
				streamServerTypeIdent := protogen.GoIdent{
					GoName:       originalGoName + "_" + method.GoName + "Server",
					GoImportPath: file.GoImportPath,
				}
				qualifiedStreamServerType = g.QualifiedGoIdent(streamServerTypeIdent)
			}

			inputMsgIdent := method.Input.GoIdent
			prefixedInputIdent := protogen.GoIdent{
				GoName:       inputMsgIdent.GoName,
				GoImportPath: file.GoImportPath,
			}

			outputMsgIdent := method.Output.GoIdent
			prefixedOutputIdent := protogen.GoIdent{
				GoName:       outputMsgIdent.GoName,
				GoImportPath: file.GoImportPath,
			}

			svcData.Methods = append(svcData.Methods, MethodData{
				Name:                      string(method.Desc.Name()),
				GoName:                    method.GoName,
				InputType:                 g.QualifiedGoIdent(prefixedInputIdent),
				OutputType:                g.QualifiedGoIdent(prefixedOutputIdent),
				ClientStreaming:           method.Desc.IsStreamingClient(),
				ServerStreaming:           method.Desc.IsStreamingServer(),
				FullMethodName:            fullMethodName,
				QualifiedStreamServerType: qualifiedStreamServerType,
			})
		}
		allServices = append(allServices, svcData)
	}

	templateData := TemplateData{
		Filename:                  outputFilename,
		PackageName:               targetPackageName,
		Services:                  allServices,
		HTTPPort:                  httpPort,
		GRPCPort:                  grpcPort,
		HasClientStreamingMethods: hasClientStreaming(allServices),
	}

	tmpl, err := template.New("grpcmockServer").Parse(serverTemplateContent)
	if err != nil {
		return fmt.Errorf("failed to parse server template: %w", err)
	}

	var buffer strings.Builder
	if err := tmpl.Execute(&buffer, templateData); err != nil {
		return fmt.Errorf("failed to execute server template: %w", err)
	}

	g.P(buffer.String())
	return nil
}
