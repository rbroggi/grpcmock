package main

import (
	_ "embed"
	"fmt"
	"log"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

type TemplateData struct {
	Filename    string
	PackageName string
	Services    []ServiceData
	HTTPPort    string
	GRPCPort    string
}

type ServiceData struct {
	OriginalGoName                   string // Original Go service name, e.g., "CustomerService"
	MockServerStructName             string // Potentially postfixed name for the mock struct, e.g., "CustomerServiceMockServer" or "CustomerService_2MockServer"
	QualifiedUnimplementedServerType string // Fully qualified original UnimplementedServer type
	QualifiedRegisterServerFuncName  string // Fully qualified original RegisterServer function
	Methods                          []MethodData
}

type MethodData struct {
	Name                      string
	GoName                    string
	InputType                 string
	OutputType                string
	ClientStreaming           bool
	ServerStreaming           bool
	FullMethodName            string
	QualifiedStreamServerType string // Fully qualified original stream server type for this method
}

//go:embed server.tmpl
var serverTemplateContent string

func generateMockServer(
	gen *protogen.Plugin,
	outputFilename, targetPackageName, httpPort, grpcPort string,
) error {
	if targetPackageName == "" {
		targetPackageName = "main"
	}
	g := gen.NewGeneratedFile(outputFilename, protogen.GoImportPath(targetPackageName))

	allServices := []ServiceData{}
	serviceGoNameCounts := make(map[string]int) // Track occurrences of original Go service names
	processedFileCount := 0

	// First pass: Collect all service definitions and determine their unique mock server struct names
	// We need to collect them first to apply postfixes correctly based on final counts.
	type pendingService struct {
		file    *protogen.File
		service *protogen.Service
	}
	var pendingServices []pendingService

	for _, file := range gen.Files {
		if !file.Generate || len(file.Services) == 0 {
			continue
		}
		processedFileCount++
		for _, service := range file.Services {
			pendingServices = append(pendingServices, pendingService{file: file, service: service})
			serviceGoNameCounts[service.GoName]++
		}
	}

	if processedFileCount == 0 {
		log.Println("grpcmock: No services found in .proto files to generate a mock server.")
		return nil
	}

	// Second pass: Generate ServiceData with unique names
	// Tracks how many times a base name has been used for MockServerStructName
	serviceFinalNameTracker := make(map[string]int)

	for _, ps := range pendingServices {
		file := ps.file
		service := ps.service

		originalGoName := service.GoName

		currentCount := serviceFinalNameTracker[originalGoName]
		currentCount++
		serviceFinalNameTracker[originalGoName] = currentCount

		mockServerStructNameBase := originalGoName                      // Base for the struct (e.g., CustomerService)
		mockServerStructName := mockServerStructNameBase + "MockServer" // e.g. CustomerServiceMockServer
		if serviceGoNameCounts[originalGoName] > 1 {                    // If this name appears more than once across all files
			// e.g. CustomerServiceMockServer2
			mockServerStructName = fmt.Sprintf("%s%d", mockServerStructName, currentCount)
		}

		unimplementedServerTypeIdent := protogen.GoIdent{
			GoName:       "Unimplemented" + originalGoName + "Server", // Based on original service name
			GoImportPath: file.GoImportPath,
		}
		registerServerFuncIdent := protogen.GoIdent{
			GoName:       "Register" + originalGoName + "Server", // Based on original service name
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
					GoName:       originalGoName + "_" + method.GoName + "Server", // Stream type uses original service name
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
		Filename:    outputFilename,
		PackageName: targetPackageName,
		Services:    allServices,
		HTTPPort:    httpPort,
		GRPCPort:    grpcPort,
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
