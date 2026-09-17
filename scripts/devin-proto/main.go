// Command devin-proto updates committed wire types from a Devin Desktop release.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const protocVersion = "libprotoc 34.1"
const pluginVersion = "protoc-gen-go v1.36.10"

var generated = map[string]string{
	"exa/api_server_pb/api_server.proto":           "internal/providers/devin/devinpb/api_server_pb/api_server.pb.go",
	"exa/chat_pb/chat.proto":                       "internal/providers/devin/devinpb/chat_pb/chat.pb.go",
	"exa/codeium_common_pb/codeium_common.proto":   "internal/providers/devin/devinpb/codeium_common_pb/codeium_common.pb.go",
	"exa/cortex_pb/cortex.proto":                   "internal/providers/devin/devinpb/cortex_pb/cortex.pb.go",
	"exa/seat_management_pb/seat_management.proto": "internal/providers/devin/devinpb/seat_management_pb/seat_management.pb.go",
}

var roots = []string{
	".exa.api_server_pb.GetChatMessageRequest", ".exa.api_server_pb.GetChatMessageResponse",
	".exa.seat_management_pb.GetUserStatusRequest", ".exa.seat_management_pb.GetUserStatusResponse",
}

func main() {
	version := flag.String("version", "", "Desktop version to extract (default: latest stable)")
	flag.Parse()
	if flag.NArg() != 0 {
		fatal(errors.New("unexpected arguments; use --version VERSION or no arguments for latest stable"))
	}
	if err := run(*version); err != nil {
		fatal(err)
	}
}

func run(version string) error {
	if err := requireVersion("protoc", protocVersion); err != nil {
		return err
	}
	if err := requireVersion("protoc-gen-go", pluginVersion); err != nil {
		return err
	}
	root, err := repositoryRoot()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "devin-proto-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	source, err := downloadDesktop(ctx, version, work)
	if err != nil {
		return err
	}
	fmt.Printf("Extracting descriptors from Desktop %s\n", source.Version)
	set, err := extractDescriptors(source.BinaryPath)
	if err != nil {
		return err
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "descriptors.pb"), raw, 0o600); err != nil {
		return err
	}
	reduced, err := selectClosure(set)
	if err != nil {
		return err
	}
	if err := validateMappings(reduced); err != nil {
		return err
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(reduced)
	if err != nil {
		return err
	}
	descriptor := filepath.Join(work, "selected-descriptors.pb")
	if err := os.WriteFile(descriptor, b, 0o600); err != nil {
		return err
	}
	output := filepath.Join(work, "out")
	if err := generate(descriptor, output); err != nil {
		return err
	}
	header := fmt.Sprintf("// Devin Desktop %s, linux-x64: %s\n// Archive SHA256 (locally calculated): %s\n// Language server SHA256: %s\n// Extracted descriptor set SHA256: %x\n// Regenerate: bash scripts/update-devin-proto.sh --version %s\n// Recovered from Desktop, not an official proto source release or a CLI compatibility guarantee.\n\n",
		source.Version, source.URL, source.ArchiveSHA256, source.BinarySHA256, sha256.Sum256(raw), source.Version)
	if err := writeGenerated(root, output, header); err != nil {
		return err
	}
	fmt.Printf("Updated %d Go files from %d extracted descriptor files; temporary artifacts removed on exit.\n", len(generated), len(set.File))
	return nil
}

func repositoryRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locate generator source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func validateMappings(set *descriptorpb.FileDescriptorSet) error {
	for _, file := range set.File {
		name := file.GetName()
		if strings.HasPrefix(name, "exa/") {
			if _, ok := generated[name]; !ok {
				return fmt.Errorf("reachable Desktop file %q has no Go package mapping", name)
			}
			continue
		}
		if name != "google/protobuf/timestamp.proto" {
			return fmt.Errorf("unsupported reachable non-Desktop file %q", name)
		}
	}
	return nil
}

type definition struct {
	file, top  string
	topMessage *descriptorpb.DescriptorProto
}

func selectClosure(source *descriptorpb.FileDescriptorSet) (*descriptorpb.FileDescriptorSet, error) {
	definitions := map[string]definition{}
	for _, file := range source.File {
		prefix := "." + file.GetPackage()
		for _, message := range file.MessageType {
			indexMessage(definitions, file.GetName(), message.GetName(), prefix, message, message)
		}
		for _, enum := range file.EnumType {
			definitions[prefix+"."+enum.GetName()] = definition{file: file.GetName(), top: enum.GetName()}
		}
	}
	selected := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if selected[name] {
			return nil
		}
		definition, ok := definitions[name]
		if !ok {
			return fmt.Errorf("selected type %q is absent from Desktop descriptors", name)
		}
		selected[name] = true
		if definition.topMessage != nil {
			if err := visitMessageTypes(definition.topMessage, visit); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	keptTop := map[string]map[string]bool{}
	requiredImports := map[string]map[string]bool{}
	for name := range selected {
		definition := definitions[name]
		if keptTop[definition.file] == nil {
			keptTop[definition.file] = map[string]bool{}
		}
		keptTop[definition.file][definition.top] = true
		if definition.topMessage != nil {
			for _, field := range messageFields(definition.topMessage) {
				if typeName := field.GetTypeName(); typeName != "" && definitions[typeName].file != definition.file {
					if requiredImports[definition.file] == nil {
						requiredImports[definition.file] = map[string]bool{}
					}
					requiredImports[definition.file][definitions[typeName].file] = true
				}
			}
		}
	}
	result := &descriptorpb.FileDescriptorSet{File: make([]*descriptorpb.FileDescriptorProto, 0, len(source.File))}
	for _, file := range source.File {
		top := keptTop[file.GetName()]
		if top == nil {
			continue
		}
		copy := proto.Clone(file).(*descriptorpb.FileDescriptorProto)
		copy.MessageType = filterMessages(copy.MessageType, top)
		copy.EnumType = filterEnums(copy.EnumType, top)
		copy.Service = filterServices(copy.Service, selected)
		dependencies, public, weak, err := filterDependencies(copy, requiredImports[file.GetName()])
		if err != nil {
			return nil, err
		}
		copy.Dependency, copy.PublicDependency, copy.WeakDependency = dependencies, public, weak
		result.File = append(result.File, copy)
	}
	return result, nil
}

func visitMessageTypes(message *descriptorpb.DescriptorProto, visit func(string) error) error {
	for _, field := range messageFields(message) {
		if typeName := field.GetTypeName(); typeName != "" {
			if err := visit(typeName); err != nil {
				return err
			}
		}
	}
	return nil
}

func messageFields(message *descriptorpb.DescriptorProto) []*descriptorpb.FieldDescriptorProto {
	fields := append([]*descriptorpb.FieldDescriptorProto(nil), message.Field...)
	for _, nested := range message.NestedType {
		fields = append(fields, messageFields(nested)...)
	}
	return fields
}

func filterDependencies(file *descriptorpb.FileDescriptorProto, keep map[string]bool) ([]string, []int32, []int32, error) {
	filtered := make([]string, 0, len(file.Dependency))
	indices := map[int32]int32{}
	for old, dependency := range file.Dependency {
		if keep[dependency] {
			indices[int32(old)] = int32(len(filtered))
			filtered = append(filtered, dependency)
		}
	}
	remap := func(values []int32) ([]int32, error) {
		mapped := make([]int32, 0, len(values))
		for _, old := range values {
			if old < 0 || int(old) >= len(file.Dependency) {
				return nil, fmt.Errorf("selected file %q has invalid dependency index %d", file.GetName(), old)
			}
			new, ok := indices[old]
			if !ok {
				return nil, fmt.Errorf("selected file %q has public or weak dependency %q outside its type closure", file.GetName(), file.Dependency[old])
			}
			mapped = append(mapped, new)
		}
		return mapped, nil
	}
	public, err := remap(file.PublicDependency)
	if err != nil {
		return nil, nil, nil, err
	}
	weak, err := remap(file.WeakDependency)
	if err != nil {
		return nil, nil, nil, err
	}
	return filtered, public, weak, nil
}

func indexMessage(definitions map[string]definition, file, top, prefix string, message, topMessage *descriptorpb.DescriptorProto) {
	name := prefix + "." + message.GetName()
	definitions[name] = definition{file: file, top: top, topMessage: topMessage}
	for _, nested := range message.NestedType {
		indexMessage(definitions, file, top, name, nested, topMessage)
	}
	for _, enum := range message.EnumType {
		definitions[name+"."+enum.GetName()] = definition{file: file, top: top, topMessage: topMessage}
	}
}

func filterMessages(messages []*descriptorpb.DescriptorProto, keep map[string]bool) []*descriptorpb.DescriptorProto {
	var filtered []*descriptorpb.DescriptorProto
	for _, message := range messages {
		if keep[message.GetName()] {
			filtered = append(filtered, message)
		}
	}
	return filtered
}

func filterEnums(enums []*descriptorpb.EnumDescriptorProto, keep map[string]bool) []*descriptorpb.EnumDescriptorProto {
	var filtered []*descriptorpb.EnumDescriptorProto
	for _, enum := range enums {
		if keep[enum.GetName()] {
			filtered = append(filtered, enum)
		}
	}
	return filtered
}

func filterServices(services []*descriptorpb.ServiceDescriptorProto, selected map[string]bool) []*descriptorpb.ServiceDescriptorProto {
	var filtered []*descriptorpb.ServiceDescriptorProto
	for _, service := range services {
		var methods []*descriptorpb.MethodDescriptorProto
		for _, method := range service.Method {
			if selected[method.GetInputType()] && selected[method.GetOutputType()] {
				methods = append(methods, method)
			}
		}
		if len(methods) != 0 {
			copy := proto.Clone(service).(*descriptorpb.ServiceDescriptorProto)
			copy.Method = methods
			filtered = append(filtered, copy)
		}
	}
	return filtered
}

func generate(descriptor, output string) error {
	if err := requireVersion("protoc", protocVersion); err != nil {
		return err
	}
	if err := requireVersion("protoc-gen-go", pluginVersion); err != nil {
		return err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	args := []string{
		"--descriptor_set_in=" + descriptor, "--go_out=" + output, "--go_opt=paths=import", "--go_opt=module=github.com/caigee-cmd/cli2api",
		"--go_opt=Mexa/api_server_pb/api_server.proto=github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/api_server_pb",
		"--go_opt=Mexa/chat_pb/chat.proto=github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/chat_pb",
		"--go_opt=Mexa/codeium_common_pb/codeium_common.proto=github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/codeium_common_pb",
		"--go_opt=Mexa/cortex_pb/cortex.proto=github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/cortex_pb",
		"--go_opt=Mexa/seat_management_pb/seat_management.proto=github.com/caigee-cmd/cli2api/internal/providers/devin/devinpb/seat_management_pb",
		"--go_opt=Mgoogle/protobuf/timestamp.proto=google.golang.org/protobuf/types/known/timestamppb",
	}
	files := make([]string, 0, len(generated))
	for file := range generated {
		files = append(files, file)
	}
	sort.Strings(files)
	command := exec.Command("protoc", append(args, files...)...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func requireVersion(command, want string) error {
	output, err := exec.Command(command, "--version").CombinedOutput()
	if err != nil {
		return err
	}
	if got := strings.TrimSpace(string(output)); got != want {
		return fmt.Errorf("%s version is %q, want %q", command, got, want)
	}
	return nil
}

func writeGenerated(root, output, header string) error {
	contents := make(map[string][]byte, len(generated))
	for _, destination := range generated {
		b, err := os.ReadFile(filepath.Join(output, destination))
		if err != nil {
			return err
		}
		contents[destination] = append([]byte(header), b...)
	}
	for destination, b := range contents {
		path := filepath.Join(root, destination)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "update devin protobuf:", err); os.Exit(1) }
