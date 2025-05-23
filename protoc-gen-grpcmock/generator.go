package main

import (
	_ "embed"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

// TemplateData holds all the information needed to render the server.tmpl
type TemplateData struct {
	Filename         string
	PackageName      string
	Services         []ServiceData
	HTTPPort         string
	GRPCPort         string
	Imports          map[string]string // Alias -> Path
	PluginModulePath string            // Go module path of this plugin, e.g., "your.domain/grpcmock"
}

// ServiceData holds information about a single gRPC service
type ServiceData struct {
	Name               string // Original service name from proto
	GoName             string // Go-idiomatic service name
	GoPackageNameToken string // Package alias for this service's own generated types (e.g., "departmentv1")
	Methods            []MethodData
}

// MethodData holds information about a single gRPC method
type MethodData struct {
	Name            string // Original method name
	GoName          string // Go-idiomatic method name
	InputType       string // Fully qualified Go type for input
	OutputType      string // Fully qualified Go type for output
	ClientStreaming bool
	ServerStreaming bool
	FullMethodName  string // e.g., /packageName.ServiceName/MethodName
}

//go:embed server.tmpl
var serverTemplateContent string

const currentPluginModulePath = "github.com/rbroggi/grpcmock"

func generateFile(gen *protogen.Plugin, file *protogen.File, httpPort, grpcPort string) {
	if len(file.Services) == 0 {
		return
	}

	// Using file.GoPackageName for the generated file's package name directly.
	// The output filename construction using 'pkg' was a bit convoluted.
	// protogen.GeneratedFile's second arg (importPath) and file.GoPackageName handle this.
	// Buf will place it based on 'out' and 'paths=source_relative' (if used).
	// A simple "server.go" in the package directory is often what's expected.
	filename := filepath.Join(filepath.Dir(file.GeneratedFilenamePrefix), "server.go")

	g := gen.NewGeneratedFile(filename, file.GoImportPath)

	templateData := TemplateData{
		Filename:         file.Desc.Path(),
		PackageName:      string(file.GoPackageName), // Package for the generated server.go
		HTTPPort:         httpPort,
		GRPCPort:         grpcPort,
		Imports:          make(map[string]string),
		PluginModulePath: currentPluginModulePath, // Use the determined module path
	}

	// Standard imports needed by the template that are not part of user protos
	// The runtime import is handled by PluginModulePath.
	// Others like "context", "log" etc., are directly in the template.

	// Collect service and method data
	for _, service := range file.Services {
		svcData := ServiceData{
			Name:               service.GoName,
			GoName:             service.GoName,
			GoPackageNameToken: string(file.GoPackageName), // The package where UnimplementedXServer and RegisterXServer live
		}
		for _, method := range service.Methods {
			fullMethodName := fmt.Sprintf("/%s.%s/%s", file.Desc.Package(), service.Desc.Name(), method.Desc.Name())

			// Add imports for message types if they are from different packages
			// (protogen.QualifiedGoIdent handles this implicitly by including the package name if needed)
			// The template's import block will range over templateData.Imports.
			// We need to ensure that packages for request/response types are added if they differ
			// from the current file's package AND are not standard types handled by g.QualifiedGoIdent automatically.

			// Example: if method.Input.GoIdent.GoImportPath is different from file.GoImportPath,
			// protogen.QualifiedGoIdent will produce "alias.Type".
			// The template needs to know to import "alias" pointing to that path.
			// This logic was slightly off before. Let's ensure imports are correctly managed.

			// The existing template directly uses g.QualifiedGoIdent, which prefixes with the necessary alias.
			// The template then needs to import these aliases. `protogen` usually handles this by adding imports
			// to the generated file `g`. The `templateData.Imports` was an attempt to manually manage this,
			// which might conflict with `protogen`'s own import management.
			// For now, let's rely on g.QualifiedGoIdent and assume protogen adds necessary imports
			// for types from other packages. The template's manual import range might be redundant or incorrect
			// if protogen handles it.

			// Clearing templateData.Imports for now as g.QualifiedGoIdent should make types resolvable
			// assuming protoc-gen-go has already run and protogen knows about these types.
			// If specific aliases are needed for the template itself, they should be added carefully.
			// The main import needed by the template explicitly is the runtime.

			svcData.Methods = append(svcData.Methods, MethodData{
				Name:            string(method.Desc.Name()),
				GoName:          method.GoName,
				InputType:       g.QualifiedGoIdent(method.Input.GoIdent),  // e.g., "pkgalias.RequestType" or "RequestType"
				OutputType:      g.QualifiedGoIdent(method.Output.GoIdent), // e.g., "pkgalias.ResponseType" or "ResponseType"
				ClientStreaming: method.Desc.IsStreamingClient(),
				ServerStreaming: method.Desc.IsStreamingServer(),
				FullMethodName:  fullMethodName,
			})
		}
		templateData.Services = append(templateData.Services, svcData)
	}

	// Define custom template functions (if still needed after simplifying imports)
	funcMap := template.FuncMap{
		"BaseType": func(qualifiedType string) string { // "pkg.Type" -> "Type", "*pkg.Type" -> "Type"
			noStar := strings.TrimPrefix(qualifiedType, "*")
			parts := strings.Split(noStar, ".")
			return parts[len(parts)-1]
		},
		"TrimStar": func(typeName string) string { // "*pkg.Type" -> "pkg.Type"
			return strings.TrimPrefix(typeName, "*")
		},
		// GoPackageNameToken is now a field in ServiceData. The GoPackageName function below is different.
		// This GoPackageName function extracts the package alias from a qualified type string.
		"GoPackageName": func(qualifiedType string) string { // "pkg.Type" -> "pkg", "*pkg.Type" -> "pkg"
			noStar := strings.TrimPrefix(qualifiedType, "*")
			parts := strings.Split(noStar, ".")
			if len(parts) > 1 {
				return parts[0]
			}
			return "" // No package qualifier, implies current package
		},
	}

	tmpl, err := template.New("server").Funcs(funcMap).Parse(serverTemplateContent)
	if err != nil {
		// Log the error to stderr which protoc will display
		log.Fatalf("grpcmock: failed to parse template: %v", err) // Use log.Fatalf for plugin errors
		return
	}

	var buffer strings.Builder
	if err := tmpl.Execute(&buffer, templateData); err != nil {
		log.Fatalf("grpcmock: failed to execute template: %v", err)
		return
	}

	g.P(buffer.String())
}

func problematicGoPackageName(file *protogen.File) string {
	if file.GoPackageName != "" {
		return string(file.GoPackageName)
	}
	return strings.ReplaceAll(string(file.Proto.GetPackage()), ".", "_")
}
