package run

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestAffectedServicesByChange(t *testing.T) {
	root := t.TempDir()
	targets := []watchTarget{
		{name: "alpha", dir: filepath.Join(root, "app", "alpha", "service")},
		{name: "beta", dir: filepath.Join(root, "app", "beta", "service")},
	}

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "service source affects only that service",
			path: filepath.Join(root, "app", "alpha", "service", "internal", "data", "user.go"),
			want: []string{"alpha"},
		},
		{
			name: "service config affects only that service",
			path: filepath.Join(root, "app", "alpha", "service", "configs", "config.yaml"),
			want: []string{"alpha"},
		},
		{
			name: "module-level shared code affects all services",
			path: filepath.Join(root, "pkg", "shared.go"),
			want: []string{"alpha", "beta"},
		},
		{
			name: "module root file affects all services",
			path: filepath.Join(root, "main.go"),
			want: []string{"alpha", "beta"},
		},
		{
			name: "change under an unwatched service affects all services",
			path: filepath.Join(root, "app", "gamma", "service", "x.go"),
			want: []string{"alpha", "beta"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := affectedServicesByChange(tt.path, root, targets)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("affected = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestShouldTriggerWatch(t *testing.T) {
	tests := []struct {
		name string
		path string
		op   fsnotify.Op
		want bool
	}{
		{name: "go source", path: "internal/data/user.go", op: fsnotify.Write, want: true},
		{name: "yaml config", path: "configs/config.yaml", op: fsnotify.Create, want: true},
		{name: "yml config", path: "configs/config.yml", op: fsnotify.Write, want: true},
		{name: "proto source", path: "api/user/v1/user.proto", op: fsnotify.Write, want: true},
		{name: "json config", path: "configs/config.json", op: fsnotify.Write, want: true},
		{name: "test file ignored", path: "internal/data/user_test.go", op: fsnotify.Write, want: false},
		{name: "hidden file ignored", path: ".main.go.swp", op: fsnotify.Write, want: false},
		{name: "untracked extension ignored", path: "README.md", op: fsnotify.Write, want: false},
		{name: "chmod ignored", path: "main.go", op: fsnotify.Chmod, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldTriggerWatch(tt.path, tt.op); got != tt.want {
				t.Fatalf("shouldTriggerWatch(%q, %v) = %v, want %v", tt.path, tt.op, got, tt.want)
			}
		})
	}
}

func TestDeriveWatchServiceName(t *testing.T) {
	if got := deriveWatchServiceName(filepath.Join("proj", "app", "alpha", "service")); got != "alpha" {
		t.Fatalf("app/<name>/service layout: got %q, want alpha", got)
	}
	if got := deriveWatchServiceName(filepath.Join("proj", "myservice")); got != "myservice" {
		t.Fatalf("flat layout: got %q, want myservice", got)
	}
}
