package main

import (
	_ "embed"
	"fmt"
	"log"
	// "path/filepath" // No longer needed for single output filename
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

type TemplateData struct {
	Filename         string
	PackageName      string
	Services         []ServiceData
	HTTPPort         string
	GRPCPort         string
	PluginModulePath string
}

type ServiceData struct {
	Name                             string // Original service name from proto, e.g., "CustomerService"
	GoName                           string // Go-idiomatic service name, e.g., "CustomerService"
	QualifiedUnimplementedServerType string // Fully qualified, e.g., "customerv1.UnimplementedCustomerServiceServer"
	QualifiedRegisterServerFuncName  string // Fully qualified, e.g., "customerv1.RegisterCustomerServiceServer"
	Methods                          []MethodData
}

type MethodData struct {
	Name                      string // Original method name, e.g., "GetDetails"
	GoName                    string // Go-idiomatic method name, e.g., "GetDetails"
	InputType                 string // Fully qualified Go type for input, e.g., "customerv1.GetCustomerDetailsRequest"
	OutputType                string // Fully qualified Go type for output, e.g., "customerv1.GetCustomerDetailsResponse"
	ClientStreaming           bool
	ServerStreaming           bool
	FullMethodName            string // e.g., /company_services.customer.v1.CustomerService/GetDetails
	QualifiedStreamServerType string // Fully qualified stream server type, e.g., "customerv1.CustomerService_GetDetailsServer"
}

//go:embed server.tmpl
var serverTemplateContent string

const currentPluginModulePath = "github.com/rbroggi/grpcmock" // From your go.mod

func generateMockServer(gen *protogen.Plugin, outputFilename, targetPackageName, httpPort, grpcPort string) error {
	if targetPackageName == "" {
		targetPackageName = "main"
	}
	g := gen.NewGeneratedFile(outputFilename, protogen.GoImportPath(targetPackageName))

	allServices := []ServiceData{}
	processedFileCount := 0

	for _, file := range gen.Files {
		if !file.Generate || len(file.Services) == 0 {
			continue
		}
		processedFileCount++

		for _, service := range file.Services {
			unimplementedServerTypeIdent := protogen.GoIdent{
				GoName:       "Unimplemented" + service.GoName + "Server",
				GoImportPath: file.GoImportPath,
			}
			registerServerFuncIdent := protogen.GoIdent{
				GoName:       "Register" + service.GoName + "Server",
				GoImportPath: file.GoImportPath,
			}

			svcData := ServiceData{
				Name:                             service.GoName,
				GoName:                           service.GoName,
				QualifiedUnimplementedServerType: g.QualifiedGoIdent(unimplementedServerTypeIdent),
				QualifiedRegisterServerFuncName:  g.QualifiedGoIdent(registerServerFuncIdent),
			}

			for _, method := range service.Methods {
				fullMethodName := fmt.Sprintf("/%s.%s/%s", file.Desc.Package(), service.Desc.Name(), method.Desc.Name())

				var qualifiedStreamServerType string
				if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
					streamServerTypeIdent := protogen.GoIdent{
						GoName:       service.GoName + "_" + method.GoName + "Server",
						GoImportPath: file.GoImportPath,
					}
					qualifiedStreamServerType = g.QualifiedGoIdent(streamServerTypeIdent)
				}

				svcData.Methods = append(svcData.Methods, MethodData{
					Name:                      string(method.Desc.Name()),
					GoName:                    method.GoName,
					InputType:                 g.QualifiedGoIdent(method.Input.GoIdent),
					OutputType:                g.QualifiedGoIdent(method.Output.GoIdent),
					ClientStreaming:           method.Desc.IsStreamingClient(),
					ServerStreaming:           method.Desc.IsStreamingServer(),
					FullMethodName:            fullMethodName,
					QualifiedStreamServerType: qualifiedStreamServerType,
				})
			}
			allServices = append(allServices, svcData)
		}
	}

	if processedFileCount == 0 {
		log.Println("grpcmock: No services found in .proto files to generate a mock server.")
		return nil
	}

	templateData := TemplateData{
		Filename:         outputFilename,
		PackageName:      targetPackageName,
		Services:         allServices,
		HTTPPort:         httpPort,
		GRPCPort:         grpcPort,
		PluginModulePath: currentPluginModulePath,
	}

	funcMap := template.FuncMap{}

	tmpl, err := template.New("grpcmockServer").Funcs(funcMap).Parse(serverTemplateContent)
	if err != nil {
		return fmt.Errorf("failed to parse server template: %w", err)
	}

	var buffer strings.Builder
	if err := tmpl.Execute(&buffer, templateData); err != nil {
		return fmt.Errorf("failed to execute server template: %w", err)
	}

	g.P(buffer.String()) // g (protogen.GeneratedFile) handles adding necessary imports
	return nil
}
