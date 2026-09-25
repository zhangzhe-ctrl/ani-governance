package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListServiceNames(t *testing.T) {
	root := t.TempDir()

	// 有效服务:alpha 同时具备 cmd/server/main.go 与 configs。
	validRoot := filepath.Join(root, "app", "alpha", "service")
	if err := os.MkdirAll(filepath.Join(validRoot, "cmd", "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(validRoot, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(validRoot, "cmd", "server", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 无效服务:beta 缺 configs;gamma 仅为普通目录;delta 为文件。
	for _, d := range []string{
		filepath.Join(root, "app", "beta", "service", "cmd", "server"),
		filepath.Join(root, "app", "gamma", "other"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(validRoot, "cmd", "server", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "delta"), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err := ListServiceNames(root)
	if err != nil {
		t.Fatalf("ListServiceNames: %v", err)
	}
	if len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", names)
	}
}

func TestListServiceNamesNoAppDir(t *testing.T) {
	if _, err := ListServiceNames(t.TempDir()); err == nil {
		t.Fatal("expected error for missing app directory")
	}
}
