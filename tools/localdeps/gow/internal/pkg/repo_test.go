package pkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRepo(t *testing.T) {
	urls := []string{
		// ssh://[user@]host.xz[:port]/path/to/repo.git/
		"ssh://git@github.com:7875/go-kratos/kratos.git",
		// git://host.xz[:port]/path/to/repo.git/
		"git://github.com:7875/go-kratos/kratos.git",
		// http[s]://host.xz[:port]/path/to/repo.git/
		"https://github.com:7875/go-kratos/kratos.git",
		// ftp[s]://host.xz[:port]/path/to/repo.git/
		"ftps://github.com:7875/go-kratos/kratos.git",
		//[user@]host.xz:path/to/repo.git/
		"git@github.com:go-kratos/kratos.git",
		// ssh://[user@]host.xz[:port]/~[user]/path/to/repo.git/
		"ssh://git@github.com:7875/go-kratos/kratos.git",
		// git://host.xz[:port]/~[user]/path/to/repo.git/
		"git://github.com:7875/go-kratos/kratos.git",
		//[user@]host.xz:/~[user]/path/to/repo.git/
		"git@github.com:go-kratos/kratos.git",
		///path/to/repo.git/
		"//github.com/go-kratos/kratos.git",
		// file:///path/to/repo.git/
		"file://./github.com/go-kratos/kratos.git",
	}
	for _, url := range urls {
		dir := repoDir(url)
		if dir != "github.com/go-kratos" && dir != "/go-kratos" {
			t.Fatal(url, "repoDir test failed", dir)
		}
	}
}

func TestRepoClone(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-b", "main", source)
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module github.com/go-kratos/service-layout\n\ngo 1.26.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("-C", source, "add", "go.mod")
	runGit("-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
	r := NewRepo(source, "")
	if err := r.Clone(context.Background()); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(root, "copy")
	if err := r.CopyTo(context.Background(), to, "github.com/go-kratos/kratos-layout", nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(to, "go.mod"))
	if err != nil || string(got) != "module github.com/go-kratos/kratos-layout\n\ngo 1.26.7\n" {
		t.Fatalf("copied module: %q, %v", got, err)
	}
}
