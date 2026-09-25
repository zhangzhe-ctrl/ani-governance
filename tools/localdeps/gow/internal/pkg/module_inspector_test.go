package pkg

import (
	"context"
	"testing"
)

func TestNewModuleInspectorFromGo(t *testing.T) {
	inspector, err := NewModuleInspectorFromGo(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Root: %s, ModPath: %s", inspector.Root, inspector.ModPath)
}
