package main

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

// TemplateData holds all the information needed to render the server.tmpl
type TemplateData struct {
	Filename    string
	PackageName string
	Services    []ServiceData
	HTTPPort    string
	GRPCPort    string
	Imports     map[string]string // Alias -> Path
}

// ServiceData holds information about a single gRPC service
type ServiceData struct {
	Name    string // Original service name from proto
	GoName  string // Go-idiomatic service name
	Methods []MethodData
}

// MethodData holds information about a single gRPC method
type MethodData struct {
	Name            string // Original method name
	GoName          string // Go-idiomatic method name
	InputType       string
	OutputType      string
	ClientStreaming bool
	ServerStreaming bool
	FullMethodName  string // e.g., /packageName.ServiceName/MethodName
}

//go:embed server.tmpl
var serverTemplateContent string

func generateFile(gen *protogen.Plugin, file *protogen.File, httpPort, grpcPort string) {
	if len(file.Services) == 0 {
		return
	}

	// Determine the output filename
	// Buf typically handles placing files based on `out` and `opt` in buf.gen.yaml.
	// The plugin should generate a filename relative to the `out` directory.
	// Example: if service is in "proto/v1/greeter.proto", output might be "proto/v1/server.go"
	// Or, more simply, put it in a package structure derived from the proto package.

	// Let's use a simple naming convention for the generated file: server.go within a directory matching the proto package.
	// Buf's `out` parameter will define the root of this structure.
	// Example: out: gen/go, proto package: acme.user.v1 -> gen/go/acme/user/v1/server.go
	// The protogen.GeneratedFile's second argument is the import path.
	// We construct the filename based on the Go package name.

	pkg := problematicGoPackageName(file) // Use helper to get a valid Go package name

	// Path for the generated file. protogen.Plugin handles the full path construction.
	// We just provide a relative path including the package.
	// For example if go_package is "example.com/foo/bar", filename could be "example.com/foo/bar/server.go"
	// However, Buf will place it based on its 'out' directive.
	// So we should use a simple name like "server.go" and let Buf handle the directory structure if 'paths=source_relative' isn't used.
	// If 'paths=source_relative' is desired, the filename should be relative to the input .proto file's directory.
	// For now, let's keep it simple and assume the output directory is managed by Buf or the user.
	// The filename here is relative to the directory specified by `protoc`'s `--xxx_out` flag or Buf's `out`.

	filename := filepath.Join(strings.ReplaceAll(pkg, ".", "/"), "server.go")
	// If the go_package option has an explicit path, use that.
	// Otherwise, use the proto package name.
	// This logic might need refinement based on how Buf resolves output paths with go_package.
	goPackagePath := file.GoImportPath.String()
	if goPackagePath != "" && goPackagePath != "." {
		// Remove quotes if any from GoImportPath
		goPackagePath = strings.Trim(goPackagePath, "\"")
		// Use the last component of the go_package as the directory for the server.go
		// This is a common pattern but might need adjustment.
		// For example, if go_package="example.com/api/foo/v1;foov1", goPackageDir = "v1"
		// If go_package="mypackage", goPackageDir = "mypackage"
		parts := strings.Split(strings.Split(goPackagePath, ";")[0], "/")
		dirPath := filepath.Join(parts...)
		filename = filepath.Join(dirPath, "server.go")

	} else {
		// Fallback if go_package is not well-defined for path construction.
		// This case should ideally be handled by ensuring protos have proper go_package.
		filename = filepath.Join(problematicGoPackageName(file), "server.go")
	}

	g := gen.NewGeneratedFile(filename, file.GoImportPath)

	templateData := TemplateData{
		Filename:    file.Desc.Path(),
		PackageName: string(file.GoPackageName),
		HTTPPort:    httpPort,
		GRPCPort:    grpcPort,
		Imports:     make(map[string]string),
	}

	// Add standard imports
	templateData.Imports["context"] = "context"
	templateData.Imports["fmt"] = "fmt"
	templateData.Imports["log"] = "log"
	templateData.Imports["net"] = "net"
	templateData.Imports["grpc"] = "google.golang.org/grpc"
	templateData.Imports["codes"] = "google.golang.org/grpc/codes"
	templateData.Imports["status"] = "google.golang.org/grpc/status"
	templateData.Imports["protojson"] = "google.golang.org/protobuf/encoding/protojson"
	templateData.Imports["proto"] = "google.golang.org/protobuf/proto"
	templateData.Imports["protoiface"] = "google.golang.org/protobuf/runtime/protoiface"

	for _, service := range file.Services {
		svcData := ServiceData{
			Name:   service.GoName, // Use GoName for the struct name
			GoName: service.GoName, // And for referencing in registration
		}
		for _, method := range service.Methods {
			fullMethodName := fmt.Sprintf("/%s.%s/%s", file.Desc.Package(), service.Desc.Name(), method.Desc.Name())

			// Add import for method input/output types
			// Ensure we don't try to import the current package itself in a problematic way
			inputImportPath := method.Input.GoIdent.GoImportPath.String()
			if inputImportPath != "" && inputImportPath != string(file.GoImportPath) && inputImportPath != "." {
				templateData.Imports[string(method.Input.GoIdent.GoImportPath)] = inputImportPath
			}
			outputImportPath := method.Output.GoIdent.GoImportPath.String()
			if outputImportPath != "" && outputImportPath != string(file.GoImportPath) && outputImportPath != "." {
				templateData.Imports[string(method.Output.GoIdent.GoImportPath)] = outputImportPath
			}

			svcData.Methods = append(svcData.Methods, MethodData{
				Name:            string(method.Desc.Name()),
				GoName:          method.GoName,
				InputType:       g.QualifiedGoIdent(method.Input.GoIdent),
				OutputType:      g.QualifiedGoIdent(method.Output.GoIdent),
				ClientStreaming: method.Desc.IsStreamingClient(),
				ServerStreaming: method.Desc.IsStreamingServer(),
				FullMethodName:  fullMethodName,
			})
		}
		templateData.Services = append(templateData.Services, svcData)
	}

	// Define custom template functions
	funcMap := template.FuncMap{
		"BaseType": func(qualifiedType string) string {
			parts := strings.Split(qualifiedType, ".")
			return parts[len(parts)-1]
		},
		"TrimStar": func(typeName string) string {
			return strings.TrimPrefix(typeName, "*")
		},
		"GoPackageName": func(qualifiedGoIdent string) string {
			// qualifiedGoIdent might be like "some_pkg.MyType" or just "MyType" (if in same package)
			// or "*some_pkg.MyType"
			trimmed := strings.TrimPrefix(qualifiedGoIdent, "*")
			parts := strings.Split(trimmed, ".")
			if len(parts) > 1 {
				return parts[0] // This assumes the package alias is the first part.
			}
			// If no explicit package (e.g. it's a type in the current generated package),
			// we might not need to prefix. However, for UnimplementedServer, it's usually prefixed.
			// This part needs careful handling based on how protogen creates QualifiedGoIdent for types
			// from the *same* target package vs. imported packages.
			// For types within the *same* generated file's package, GoPackageName might be empty or the package name itself.
			// The grpc server registration usually needs it like: `actualpackage.RegisterServiceServer(s, &server{})`
			// And the Unimplemented struct is `actualpackage.UnimplementedServiceServer`.
			// If the type is from an imported WKT, protogen handles it.
			// If the type is from another user proto in a different package, protogen handles it.
			// This is mainly for Unimplemented<Service>Server struct embedding.
			return templateData.PackageName // Fallback or primary for current package context.
		},
	}

	tmpl, err := template.New("server").Funcs(funcMap).Parse(serverTemplateContent)
	if err != nil {
		gen.Error(fmt.Errorf("grpcmock: failed to parse template: %v", err))
		return
	}

	// Use a buffer to write the generated code
	var buffer strings.Builder
	if err := tmpl.Execute(&buffer, templateData); err != nil {
		gen.Error(fmt.Errorf("grpcmock: failed to execute template: %v", err))
		return
	}

	// Write the generated code to the output file
	g.P(buffer.String())

}

// Helper to create a valid Go package name from a file descriptor.
// This can be complex if there are no go_package options.
func problematicGoPackageName(file *protogen.File) string {
	if file.GoPackageName != "" {
		return string(file.GoPackageName)
	}
	// Fallback to proto package name, sanitize if necessary
	// This is a simplistic fallback. `protoc-gen-go` has more sophisticated logic.
	return strings.ReplaceAll(string(file.Proto.GetPackage()), ".", "_")
}
