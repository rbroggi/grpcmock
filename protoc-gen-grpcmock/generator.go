package main

import (
	_ "embed" // Required for go:embed
	"fmt"
	"log"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

//go:embed server.tmpl
var serverTemplateContent string

// TemplateData holds all data passed to the server template for code generation.
type TemplateData struct {
	PackageName string // Go package name for the generated file (e.g., "main")
	Services    []ServiceData
	HTTPPort    string // Default HTTP port
	GRPCPort    string // Default gRPC port
}

// ServiceData holds information about a single gRPC service.
type ServiceData struct {
	OriginalGoName                   string // e.g., "CustomerService"
	MockServerStructName             string // e.g., "CustomerServiceGrpcmockServer"
	QualifiedUnimplementedServerType string // e.g., "pb.UnimplementedCustomerServiceServer"
	QualifiedRegisterServerFuncName  string // e.g., "pb.RegisterCustomerServiceServer"
	Methods                          []MethodData
}

// MethodData holds information about a single gRPC method.
type MethodData struct {
	GoName                    string // Method name in Go (e.g., "GetDetails")
	FullMethodName            string // Full gRPC method name (e.g., "/package.Service/Method")
	InputType                 string // Fully qualified Go type for input (e.g., "*pb.GetCustomerDetailsRequest")
	OutputType                string // Fully qualified Go type for output (e.g., "*pb.GetCustomerDetailsResponse")
	ClientStreaming           bool
	ServerStreaming           bool
	QualifiedStreamServerType string // e.g. "pb.UserService_ListUsersServer" - if streaming
	// Used in template to create new instances for RecvMsg for client streaming
	// and for UnmarshalMapToProto for server streaming responses.
	InputObjectTypeForNew  string // e.g. "pb.MyRequest" (without pointer) for new(pb.MyRequest)
	OutputObjectTypeForNew string // e.g. "pb.MyResponse" (without pointer) for new(pb.MyResponse)

}

func generateMockServer(
	plugin *protogen.Plugin,
	outputFilename, targetPackageName, httpPort, grpcPort string,
) error {
	if targetPackageName == "" {
		targetPackageName = "main" // Default package
	}

	// The output path for the single server file is relative to the `out` dir specified in buf.gen.yaml
	// E.g. if out is "gen/grpcmock", then this file is "gen/grpcmock/grpcmockserver.go"
	// The import path within this file will be relative to its location if it's not main.
	// For simplicity, if targetPackageName is "main", GoImportPath can be ".".
	// If targetPackageName is something else, e.g. "custommock", the GoImportPath might be
	// more complex if the generated file is deep in a directory structure.
	// Let's assume protoc-gen-grpcmock generates a single file at the root of its `out` path for now.
	var goImportPath protogen.GoImportPath
	if targetPackageName == "main" {
		goImportPath = "." // Indicates current package
	} else {
		// If users specify a package name like "foo", and output is "gen/grpcmock",
		// the file will be "gen/grpcmock/grpcmockserver.go" in package "foo".
		// The import path for types within this generated package itself is just the package name.
		// This needs to be robust. For now, assume the generated file `outputFilename`
		// directly defines `targetPackageName`.
		// The GoImportPath for protogen.GeneratedFile refers to the import path of the package
		// *being generated*.
		goImportPath = protogen.GoImportPath(targetPackageName)
	}

	g := plugin.NewGeneratedFile(outputFilename, goImportPath)

	templateData := TemplateData{
		PackageName: targetPackageName,
		Services:    []ServiceData{},
		HTTPPort:    httpPort,
		GRPCPort:    grpcPort,
	}

	serviceNameUniquefier := make(map[string]int)

	for _, file := range plugin.Files {
		if !file.Generate {
			continue
		}
		for _, service := range file.Services {
			originalGoName := service.GoName
			count := serviceNameUniquefier[originalGoName]
			serviceNameUniquefier[originalGoName] = count + 1

			mockServerStructName := originalGoName + "GrpcmockServer"
			if count > 0 { // Make struct name unique if service name appears in multiple protos
				mockServerStructName = fmt.Sprintf("%s%d", mockServerStructName, count)
			}

			svcData := ServiceData{
				OriginalGoName:       originalGoName,
				MockServerStructName: mockServerStructName,
				QualifiedUnimplementedServerType: g.QualifiedGoIdent(protogen.GoIdent{
					GoName:       "Unimplemented" + originalGoName + "Server",
					GoImportPath: file.GoImportPath,
				}),
				QualifiedRegisterServerFuncName: g.QualifiedGoIdent(protogen.GoIdent{
					GoName:       "Register" + originalGoName + "Server",
					GoImportPath: file.GoImportPath,
				}),
			}

			for _, method := range service.Methods {
				fullMethod := fmt.Sprintf("/%s/%s", service.Desc.FullName(), method.Desc.Name())

				inputStreamServerType := ""
				if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
					// Example: service Greeter, method SayHelloStream => Greeter_SayHelloStreamServer
					streamIdent := protogen.GoIdent{
						GoName:       fmt.Sprintf("%s_%sServer", service.GoName, method.GoName),
						GoImportPath: file.GoImportPath,
					}
					inputStreamServerType = g.QualifiedGoIdent(streamIdent)
				}

				// Input and Output types need to be qualified with their package
				inputIdent := method.Input.GoIdent
				outputIdent := method.Output.GoIdent

				// For new(pb.Type), we need the non-pointer, qualified name.
				// protogen.GoIdent.String() gives "pkg.Type". If it's from the same package as the generated server,
				// we might not need the qualifier. But it's safer to qualify.
				// g.QualifiedGoIdent gives "*pkg.Type". We need to strip "*".

				qualifiedInputType := g.QualifiedGoIdent(inputIdent)
				qualifiedOutputType := g.QualifiedGoIdent(outputIdent)

				inputObjectForNew := strings.TrimPrefix(qualifiedInputType, "*")
				outputObjectForNew := strings.TrimPrefix(qualifiedOutputType, "*")

				methodData := MethodData{
					GoName:                    method.GoName,
					FullMethodName:            fullMethod,
					InputType:                 qualifiedInputType,
					OutputType:                qualifiedOutputType,
					ClientStreaming:           method.Desc.IsStreamingClient(),
					ServerStreaming:           method.Desc.IsStreamingServer(),
					QualifiedStreamServerType: inputStreamServerType,
					InputObjectTypeForNew:     inputObjectForNew,
					OutputObjectTypeForNew:    outputObjectForNew,
				}
				svcData.Methods = append(svcData.Methods, methodData)
			}
			templateData.Services = append(templateData.Services, svcData)
		}
	}
	if len(templateData.Services) == 0 {
		log.Println("grpcmock-generator: No services found to generate mock server.")
		g.Skip() // Don't generate an empty file
		return nil
	}

	tmpl := template.Must(template.New("grpcmockServer").Parse(serverTemplateContent))
	if err := tmpl.Execute(g, templateData); err != nil {
		return fmt.Errorf("failed to execute server template: %w", err)
	}

	return nil
}
