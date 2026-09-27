package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeServiceFixture 在 base 下建立 app/{name}/service 的完整服务目录
// (cmd/server/main.go 与 configs)。
func makeServiceFixture(t *testing.T, base, name string) {
	t.Helper()
	svcRoot := filepath.Join(base, "app", name, "service")
	if err := os.MkdirAll(filepath.Join(svcRoot, "cmd", "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcRoot, "cmd", "server", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(svcRoot, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestExtractServiceName(t *testing.T) {
	root := t.TempDir()
	makeServiceFixture(t, root, "alpha")

	// 兄弟前缀目录:路径字符串以 root 为前缀,但并非 root 的子目录。
	sibling := root + "c"
	makeServiceFixture(t, sibling, "alpha")

	for _, tt := range []struct {
		name       string
		root       string
		dir        string
		wantName   string
		wantErrSub string
	}{
		{
			name:     "valid service directory",
			root:     root,
			dir:      filepath.Join(root, "app", "alpha", "service", "cmd", "server"),
			wantName: "alpha",
		},
		{
			name:     "root itself is not a service directory",
			root:     root,
			dir:      root,
			wantName: "",
		},
		{
			name:     "shallow path has no service name",
			root:     root,
			dir:      filepath.Join(root, "app", "alpha"),
			wantName: "",
		},
		{
			name:       "non-service segment is rejected",
			root:       root,
			dir:        filepath.Join(root, "app", "alpha", "other", "deep"),
			wantErrSub: "does not match expected structure",
		},
		{
			name:       "sibling-prefix directory is outside the root",
			root:       root,
			dir:        filepath.Join(sibling, "app", "alpha", "service", "cmd", "server"),
			wantErrSub: "not within the project root",
		},
		{
			name:       "unrelated directory is outside the root",
			root:       root,
			dir:        filepath.Join(os.TempDir(), "elsewhere", "app", "alpha", "service", "cmd", "server"),
			wantErrSub: "not within the project root",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name, err := ExtractServiceName(tt.root, tt.dir)
			if tt.wantErrSub != "" {
				if err == nil {
					t.Fatalf("ExtractServiceName(%q) = %q, nil; want error", tt.dir, name)
				}
				if !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("ExtractServiceName error = %q; want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractServiceName: %v", err)
			}
			if name != tt.wantName {
				t.Fatalf("ExtractServiceName = %q; want %q", name, tt.wantName)
			}
		})
	}
}
