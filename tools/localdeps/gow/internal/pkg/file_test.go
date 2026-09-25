package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplaceTemplateInCurrentDir 在临时目录夹具上验证模板模块名替换:
// 只有包含目标串的 .go/.mod/.yaml 会被改写;其他扩展名、不含目标串的文件、
// 以及 vendor 目录下的文件必须保持原样。
func TestReplaceTemplateInCurrentDir(t *testing.T) {
	const (
		oldModule = "github.com/tx7do/go-wind-admin-template"
		newModule = "github.com/tx7do/my-wind-admin"
	)

	root := t.TempDir()
	files := map[string]string{
		"go.mod":        "module " + oldModule + "\n",
		"main.go":       "import _ \"" + oldModule + "/pkg\"\n",
		"conf/app.yaml": oldModule + "\n",
		"notes.txt":     oldModule + "\n",
		"vendor/v.go":   "package v\n// " + oldModule + "\n",
		"untouched.go":  "package main\n",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	updateCount, err := ReplaceTemplateInCurrentDir(root, oldModule, newModule)
	if err != nil {
		t.Fatalf("ReplaceTemplateInCurrentDir: %v", err)
	}
	if updateCount != 3 {
		t.Fatalf("updateCount = %d; want 3 (go.mod, main.go, conf/app.yaml)", updateCount)
	}

	for name, body := range files {
		data, rerr := os.ReadFile(filepath.Join(root, name))
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		switch name {
		case "go.mod", "main.go", "conf/app.yaml":
			if strings.Contains(string(data), oldModule) {
				t.Fatalf("%s still contains the old module name", name)
			}
			if !strings.Contains(string(data), newModule) {
				t.Fatalf("%s missing the new module name", name)
			}
		default:
			if string(data) != body {
				t.Fatalf("%s must stay untouched, got %q", name, data)
			}
		}
	}
}
