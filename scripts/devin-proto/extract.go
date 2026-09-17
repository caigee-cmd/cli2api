package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

var protoFilename = regexp.MustCompile(`^[A-Za-z0-9_./-]+\.proto$`)

func extractDescriptors(path string) (*descriptorpb.FileDescriptorSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	files := map[string]*descriptorpb.FileDescriptorProto{}
	for search := 0; search < len(data); {
		match := bytes.Index(data[search:], []byte(".proto"))
		if match < 0 {
			break
		}
		end := search + match + len(".proto")
		search = end
		// LIMIT: embedded file names must fit in 4 KiB; revisit if a release
		// uses longer names or stops embedding ordinary serialized descriptors.
		for start := max(0, end-4096); start < end; start++ {
			if data[start] != 0x0a {
				continue
			}
			name, size := protowire.ConsumeBytes(data[start+1:])
			if size < 0 || start+1+size != end || !protoFilename.Match(name) {
				continue
			}
			file := embeddedFile(data[start:], string(name))
			if file == nil {
				continue
			}
			if previous := files[file.GetName()]; previous != nil && !proto.Equal(previous, file) {
				// Desktop embeds two versions of Google's reflection schema.
				// Keep the larger one; conflicting application schemas must fail.
				if file.GetName() != "google/protobuf/descriptor.proto" || proto.Size(previous) == proto.Size(file) {
					return nil, fmt.Errorf("conflicting embedded descriptors for %s", file.GetName())
				}
				if proto.Size(previous) > proto.Size(file) {
					continue
				}
			}
			files[file.GetName()] = file
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no embedded protobuf file descriptors found")
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, file := range files {
		set.File = append(set.File, file)
	}
	sort.Slice(set.File, func(i, j int) bool { return set.File[i].GetName() < set.File[j].GetName() })
	if _, err := protodesc.NewFiles(set); err != nil {
		return nil, fmt.Errorf("invalid or incomplete extracted schema: %w", err)
	}
	return set, nil
}

func embeddedFile(data []byte, name string) *descriptorpb.FileDescriptorProto {
	file := &descriptorpb.FileDescriptorProto{}
	fields := file.ProtoReflect().Descriptor().Fields()
	offset, previous := 0, protowire.Number(0)
	// LIMIT: expect tag-ordered descriptors; revisit if Desktop changes its
	// serializer. A reset or repeated singular field marks the next object.
	for offset < len(data) {
		number, wire, size := protowire.ConsumeField(data[offset:])
		field := fields.ByNumber(number)
		if size < 0 || field == nil || number < previous || (number == previous && !field.IsList()) {
			break
		}
		want := protowire.VarintType
		if field.Kind() == protoreflect.StringKind || field.Kind() == protoreflect.MessageKind {
			want = protowire.BytesType
		}
		if wire != want && !(field.IsList() && wire == protowire.BytesType) {
			break
		}
		offset += size
		previous = number
	}
	if proto.Unmarshal(data[:offset], file) != nil || file.GetName() != name {
		return nil
	}
	if len(file.MessageType)+len(file.EnumType)+len(file.Service)+len(file.Extension) == 0 {
		return nil
	}
	return file
}
