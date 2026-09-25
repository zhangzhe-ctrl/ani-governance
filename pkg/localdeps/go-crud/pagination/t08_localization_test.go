// 本文件是 T08 分页 Proto 本地化的合同测试，不参与 protoc-gen-go 生成。
// 它固定三件事：分页 Proto 在进程内只注册一份、注册的是本地 pkg/localdeps 描述符、
// Proto 全名/字段号/枚举值与 go_package 之外的内容保持上游形态。

package pagination

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
)

const (
	localProtoPath    = "pagination/v1/pagination.proto"
	localGoPackage    = "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	upstreamGoPackage = "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
)

func TestT08PaginationProtoIsRegisteredExactlyOnce(t *testing.T) {
	var hits []protoreflect.FileDescriptor
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Path() == localProtoPath {
			hits = append(hits, fd)
		}
		return true
	})
	if len(hits) != 1 {
		t.Fatalf("%s registered %d times; a second copy means the old BSR module is still in the graph",
			localProtoPath, len(hits))
	}

	registered := hits[0]
	inUse := (&paginationV1.PagingRequest{}).ProtoReflect().Descriptor().ParentFile()
	if registered != inUse {
		t.Fatalf("the descriptor backing the imported Go type is not the registered one")
	}

	fd := protodesc.ToFileDescriptorProto(registered)
	if got := fd.GetOptions().GetGoPackage(); got != localGoPackage {
		t.Errorf("registered go_package = %q, want %q", got, localGoPackage)
	}
	if strings.HasPrefix(fd.GetOptions().GetGoPackage(), upstreamGoPackage) {
		t.Errorf("go_package still points at the upstream module path")
	}
	if registered.Package() != "pagination" {
		t.Errorf("proto package = %q, want pagination (upstream declares package pagination)", registered.Package())
	}
	if registered.Messages().Len() != 12 || registered.Enums().Len() != 3 {
		t.Errorf("top-level shape changed: messages=%d enums=%d, want 12/3",
			registered.Messages().Len(), registered.Enums().Len())
	}
	// 上游在本文件除 go_package 外没有声明任何文件级 options 或未知扩展，
	// 这里把它当作回归闸门：一旦生成链丢失/新增了文件级 options，测试就会失败。
	if extSet(fd.GetOptions()) != 0 {
		t.Errorf("file-level option extensions = %d, want 0", extSet(fd.GetOptions()))
	}
	if u := proto.Message(fd.GetOptions()).ProtoReflect().GetUnknown(); len(u) != 0 {
		t.Errorf("file options carry %d unknown bytes, want 0", len(u))
	}

	// 依赖集合也必须本地化：任何 github.com/tx7do 的 import 路径都会把旧 Proto 带回进程。
	want := []string{
		"google/protobuf/field_mask.proto",
		"google/protobuf/wrappers.proto",
		"google/protobuf/any.proto",
		"google/protobuf/struct.proto",
		"gnostic/openapi/v3/annotations.proto",
	}
	if registered.Imports().Len() != len(want) {
		t.Fatalf("imports = %d, want %d", registered.Imports().Len(), len(want))
	}
	for i := 0; i < registered.Imports().Len(); i++ {
		path := registered.Imports().Get(i).Path()
		if path != want[i] {
			t.Errorf("import %d = %q, want %q", i, path, want[i])
		}
		if strings.HasPrefix(path, "github.com/tx7do") {
			t.Errorf("import %q references the upstream tx7do module", path)
		}
	}
}

func TestT08PagingRequestContractIsUnchanged(t *testing.T) {
	fd := (&paginationV1.PagingRequest{}).ProtoReflect().Descriptor().ParentFile()

	fields := []struct {
		name     string
		number   protoreflect.FieldNumber
		kind     protoreflect.Kind
		json     string
		oneof    string
		repeated bool
		exts     int
	}{
		{"page", 1, protoreflect.Uint32Kind, "page", "_page", false, 1},
		{"page_size", 2, protoreflect.Uint32Kind, "pageSize", "_page_size", false, 1},
		{"offset", 3, protoreflect.Uint64Kind, "offset", "_offset", false, 1},
		{"limit", 4, protoreflect.Uint32Kind, "limit", "_limit", false, 1},
		{"token", 5, protoreflect.StringKind, "token", "_token", false, 1},
		{"no_paging", 6, protoreflect.BoolKind, "noPaging", "_no_paging", false, 1},
		{"query", 10, protoreflect.StringKind, "query", "filtering_type", false, 1},
		{"filter", 11, protoreflect.StringKind, "filter", "filtering_type", false, 1},
		{"filter_expr", 12, protoreflect.MessageKind, "filterExpr", "filtering_type", false, 1},
		{"order_by", 20, protoreflect.StringKind, "orderBy", "_order_by", false, 1},
		{"sorting", 21, protoreflect.MessageKind, "sorting", "", true, 1},
		{"field_mask", 30, protoreflect.MessageKind, "fieldMask", "_field_mask", false, 1},
	}
	msg := fd.Messages().ByName("PagingRequest")
	if msg == nil {
		t.Fatal("pagination.PagingRequest is missing")
	}
	if msg.Fields().Len() != len(fields) {
		t.Fatalf("PagingRequest has %d fields, want %d", msg.Fields().Len(), len(fields))
	}
	for _, want := range fields {
		f := msg.Fields().ByNumber(want.number)
		if f == nil {
			t.Fatalf("field number %d is missing", want.number)
		}
		if string(f.Name()) != want.name {
			t.Errorf("field %d = %q, want %q", want.number, f.Name(), want.name)
		}
		if f.Kind() != want.kind {
			t.Errorf("field %s kind = %v, want %v", want.name, f.Kind(), want.kind)
		}
		if f.JSONName() != want.json {
			t.Errorf("field %s json_name = %q, want %q", want.name, f.JSONName(), want.json)
		}
		if got := f.ContainingOneof(); got != nil {
			if string(got.Name()) != want.oneof {
				t.Errorf("field %s oneof = %q, want %q", want.name, got.Name(), want.oneof)
			}
		} else if want.oneof != "" {
			t.Errorf("field %s is not in a oneof, want %q", want.name, want.oneof)
		}
		if isRep := f.Cardinality() == protoreflect.Repeated; isRep != want.repeated {
			t.Errorf("field %s repeated = %v, want %v", want.name, isRep, want.repeated)
		}
		// json_name 与 gnostic openapi property 同挂在字段 options 上：
		// 整包丢弃 options 时这里会先变 0。
		if got := extSet(f.Options()); got != want.exts {
			t.Errorf("field %s carries %d option extensions, want %d", want.name, got, want.exts)
		}
	}

	enums := map[string]map[string]int32{
		"Operator": {
			"OPERATOR_UNSPECIFIED": 0, "EQ": 1, "NEQ": 2, "GT": 3, "GTE": 4, "LT": 5, "LTE": 6,
			"LIKE": 7, "ILIKE": 8, "NOT_LIKE": 9, "IN": 10, "NIN": 11, "IS_NULL": 12, "IS_NOT_NULL": 13,
			"BETWEEN": 14, "REGEXP": 15, "IREGEXP": 16, "CONTAINS": 17, "STARTS_WITH": 18, "ENDS_WITH": 19,
			"ICONTAINS": 20, "ISTARTS_WITH": 21, "IENDS_WITH": 22, "JSON_CONTAINS": 23, "ARRAY_CONTAINS": 24,
			"EXISTS": 25, "SEARCH": 26, "EXACT": 27, "IEXACT": 28,
		},
		"DatePart": {
			"DATE_PART_UNSPECIFIED": 0, "DATE": 1, "YEAR": 2, "ISO_YEAR": 3, "QUARTER": 4, "MONTH": 5,
			"WEEK": 6, "WEEK_DAY": 7, "ISO_WEEK_DAY": 8, "DAY": 9, "TIME": 10, "HOUR": 11, "MINUTE": 12,
			"SECOND": 13, "MICROSECOND": 14,
		},
		"ExprType": {"EXPR_TYPE_UNSPECIFIED": 0, "AND": 1, "OR": 2},
	}
	for name, want := range enums {
		e := fd.Enums().ByName(protoreflect.Name(name))
		if e == nil {
			t.Fatalf("top-level enum %s is missing", name)
		}
		if e.Values().Len() != len(want) {
			t.Errorf("enum %s has %d values, want %d", name, e.Values().Len(), len(want))
		}
		for i := 0; i < e.Values().Len(); i++ {
			v := e.Values().Get(i)
			if n, ok := want[string(v.Name())]; !ok {
				t.Errorf("enum %s has unexpected value %q", name, v.Name())
			} else if int32(v.Number()) != n {
				t.Errorf("enum %s.%s = %d, want %d", name, v.Name(), v.Number(), n)
			}
		}
	}
	direction := fd.Messages().ByName("Sorting").Enums().ByName("Direction")
	if direction == nil {
		t.Fatal("pagination.Sorting.Direction is missing")
	}
	if direction.Values().ByNumber(0) == nil || direction.Values().ByName("ASC") == nil || direction.Values().ByName("DESC") == nil {
		t.Errorf("pagination.Sorting.Direction values changed: %v", direction.Values())
	}
}

func extSet(m protoreflect.ProtoMessage) int {
	var n int
	proto.Message(m).ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if fd.IsExtension() {
			n++
		}
		return true
	})
	return n
}
