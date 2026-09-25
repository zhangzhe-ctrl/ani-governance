package ent

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNewEntCmdDefaults(t *testing.T) {
	e := NewEntCmd("")
	if e.TargetDir != filepath.Clean("internal/data/ent/schema") {
		t.Fatalf("default TargetDir = %q", e.TargetDir)
	}
	if e.Timeout != 2*time.Minute {
		t.Fatalf("default Timeout = %v", e.Timeout)
	}

	// 显式目录按 filepath.Clean 归一。
	e = NewEntCmd("./app/admin/service/./internal/data/ent/schema/../schema")
	if e.TargetDir != filepath.Clean("app/admin/service/internal/data/ent/schema") {
		t.Fatalf("explicit TargetDir = %q", e.TargetDir)
	}
}

func TestRunNewRequiresNames(t *testing.T) {
	e := NewEntCmd(filepath.Join(t.TempDir(), "schema"))
	if err := e.RunNew(context.Background(), nil); err == nil {
		t.Fatal("RunNew without schema names must fail before touching the filesystem")
	}
}
