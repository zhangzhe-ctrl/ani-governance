// Package common dumps the runtime descriptor of the pagination Proto file.
//
// It is linked into two separate binaries because protobuf-go refuses to register the
// same proto path twice in one process. The dumps are then compared by cmd/compare.
// Rendering is hand-written: prototext spacing was observed to differ between the two
// link graphs, which would fake a drift that does not exist, so the FileOptions are
// additionally compared from the raw descriptor bytes embedded in the generated .pb.go.
package common

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Emit writes the canonical text dump of a file descriptor to stdout.
func Emit(fd protoreflect.FileDescriptor) {
	fdp := protodesc.ToFileDescriptorProto(fd)

	var msgs, fields, enums, enumVals int
	var walk func(protoreflect.MessageDescriptor)
	walk = func(m protoreflect.MessageDescriptor) {
		msgs++
		fields += m.Fields().Len()
		for i := 0; i < m.Enums().Len(); i++ {
			enums++
			enumVals += m.Enums().Get(i).Values().Len()
		}
		for i := 0; i < m.Messages().Len(); i++ {
			walk(m.Messages().Get(i))
		}
	}
	for i := 0; i < fd.Messages().Len(); i++ {
		walk(fd.Messages().Get(i))
	}
	for i := 0; i < fd.Enums().Len(); i++ {
		enums++
		enumVals += fd.Enums().Get(i).Values().Len()
	}
	var imps []string
	for i := 0; i < fd.Imports().Len(); i++ {
		imps = append(imps, fmt.Sprintf("%s public=%v weak=%v",
			fd.Imports().Get(i).Path(), fd.Imports().Get(i).IsPublic,
			fd.Imports().Get(i).IsWeak))
	}
	sort.Strings(imps)

	out := &strings.Builder{}
	fmt.Fprintf(out, "path=%q proto_package=%q syntax=%q services=%d extensions=%d\n",
		fd.Path(), fd.Package(), fd.Syntax(), fd.Services().Len(), fd.Extensions().Len())
	fmt.Fprintf(out, "counts messages=%d fields=%d enums=%d enumValues=%d\n",
		msgs, fields, enums, enumVals)
	fmt.Fprintf(out, "imports %v\n", imps)
	fmt.Fprintf(out, "source_code_info_registered=%v\n", fdp.SourceCodeInfo != nil)
	canonical(out, "", fdp.ProtoReflect())
	m, err := proto.Marshal(fdp)
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(out, "---- runtime re-marshal: %d bytes sha256=%x ----\n", len(m), sha256.Sum256(m))
	if _, err := os.Stdout.WriteString(out.String()); err != nil {
		panic(err)
	}
}

// canonical prints every populated field recursively, in field-number order, with a
// stable scalar rendering.
func canonical(out *strings.Builder, path string, m protoreflect.Message) {
	var keys []protoreflect.FieldDescriptor
	m.Range(func(f protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		keys = append(keys, f)
		return true
	})
	sort.Slice(keys, func(i, j int) bool { return keys[i].Number() < keys[j].Number() })
	for _, f := range keys {
		v := m.Get(f)
		name := path + string(f.Name())
		switch {
		case f.IsList():
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				if isMsg(f) {
					canonical(out, name+"["+strconv.Itoa(i)+"].", l.Get(i).Message())
				} else {
					fmt.Fprintf(out, "%s[%s]=%s\n", name, strconv.Itoa(i), scalar(l.Get(i)))
				}
			}
		case f.IsMap():
			mm := v.Map()
			var mk []string
			mm.Range(func(k protoreflect.MapKey, val protoreflect.Value) bool {
				mk = append(mk, k.String()+"\x00"+scalar(val))
				return true
			})
			sort.Strings(mk)
			for _, e := range mk {
				k, val, _ := strings.Cut(e, "\x00")
				fmt.Fprintf(out, "%s[%s]=%s\n", name, k, val)
			}
		case isMsg(f):
			canonical(out, name+".", v.Message())
		default:
			fmt.Fprintf(out, "%s=%s\n", name, scalar(v))
		}
	}
}

func isMsg(f protoreflect.FieldDescriptor) bool {
	return f.Kind() == protoreflect.MessageKind || f.Kind() == protoreflect.GroupKind
}

func scalar(v protoreflect.Value) string {
	switch x := v.Interface().(type) {
	case protoreflect.EnumNumber:
		return fmt.Sprintf("enum(%d)", int32(x))
	case []byte:
		return fmt.Sprintf("bytes(len=%d)", len(x))
	default:
		return fmt.Sprintf("%v", x)
	}
}
