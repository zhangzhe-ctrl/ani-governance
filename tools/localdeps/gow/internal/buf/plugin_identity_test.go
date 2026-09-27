package buf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The F4 rule: a tool that is merely present is not a tool that has been verified. Existence and a
// self-reported version banner are both things an impostor can arrange, so the check is the build info
// inside the binary, compared against tools/config/tool-lock.json.

func newVerifierForTest(t *testing.T, lock map[string]lockedTool) *pluginVerifier {
	t.Helper()
	return &pluginVerifier{
		root:          t.TempDir(),
		lock:          lock,
		module:        "go-wind-admin",
		verifiedPaths: map[string]string{},
		filePlugins:   map[string]error{},
	}
}

// TestPinnedToolIdentityAcceptsTheBinaryItClaimsToBe is the positive control: the recorded coordinates
// of a real Go binary pass, and moving either of them fails. Without this half the rule would look
// right while rejecting everything.
func TestPinnedToolIdentityAcceptsTheBinaryItClaimsToBe(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary is a Go binary this run can point PATH at, so the rule is exercised against
	// real build info rather than against a fixture that could never have come from a tool install.
	dir := t.TempDir()
	if err := os.Symlink(self, filepath.Join(dir, "tool")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	info, err := buildInfo(context.Background(), self)
	if err != nil {
		t.Fatalf("the test binary itself must carry build info: %v", err)
	}
	if info.Module == "" || info.PackagePath == "" {
		t.Fatalf("build info of %s is incomplete: %+v", self, info)
	}

	t.Run("accepts", func(t *testing.T) {
		v := newVerifierForTest(t, map[string]lockedTool{"tool": {Module: info.PackagePath, Version: info.Version}})
		if err := v.verifyPathTool(context.Background(), "tool"); err != nil {
			t.Fatalf("the pinned binary was rejected: %v", err)
		}
	})
	t.Run("rejects a moved version", func(t *testing.T) {
		v := newVerifierForTest(t, map[string]lockedTool{"tool": {Module: info.PackagePath, Version: info.Version + "-not"}})
		err := v.verifyPathTool(context.Background(), "tool")
		if err == nil || !strings.Contains(err.Error(), "pins") {
			t.Fatalf("a wrong pinned version must be refused, got %v", err)
		}
	})
	t.Run("rejects a moved package", func(t *testing.T) {
		v := newVerifierForTest(t, map[string]lockedTool{"tool": {Module: "some/other/package", Version: info.Version}})
		if err := v.verifyPathTool(context.Background(), "tool"); err == nil {
			t.Fatal("a binary that is not the pinned package must be refused")
		}
	})
	t.Run("rejects an unpinned name", func(t *testing.T) {
		v := newVerifierForTest(t, map[string]lockedTool{})
		err := v.verifyPathTool(context.Background(), "tool")
		if err == nil || !strings.Contains(err.Error(), "unpinned") {
			t.Fatalf("an unpinned tool must be refused, got %v", err)
		}
	})
	t.Run("rejects something that is not executable", func(t *testing.T) {
		script := filepath.Join(dir, "notatool")
		if err := os.WriteFile(script, []byte("#!/bin/sh\necho notatool v9.9.9\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		v := newVerifierForTest(t, map[string]lockedTool{"notatool": {Module: info.PackagePath, Version: info.Version}})
		if err := v.verifyPathTool(context.Background(), "notatool"); err == nil {
			t.Fatal("a file nobody can execute was accepted as a tool")
		}
		// The same file named directly, which is how a `local: ../path` plugin arrives, has to fail on
		// the executable check rather than on PATH resolution.
		if _, err := buildInfo(context.Background(), script); err == nil || !strings.Contains(err.Error(), "not a regular executable") {
			t.Fatalf("buildInfo accepted a non-executable file: %v", err)
		}
	})
}

// TestImpostorWithAWrongVersionBannerIsRejected is F4's second counterexample turned into an
// assertion: a shell script on PATH that prints whatever version it likes is not the pinned plugin,
// and the banner is never consulted as a substitute for identity.
func TestImpostorWithAWrongVersionBannerIsRejected(t *testing.T) {
	dir := t.TempDir()
	impostor := filepath.Join(dir, "protoc-gen-go")
	if err := os.WriteFile(impostor, []byte("#!/bin/sh\nprintf 'protoc-gen-go v0.0.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	v := newVerifierForTest(t, map[string]lockedTool{
		"protoc-gen-go": {Module: "google.golang.org/protobuf/cmd/protoc-gen-go", Version: "v1.36.11"},
	})
	err := v.verifyPathTool(context.Background(), "protoc-gen-go")
	if err == nil {
		t.Fatal("a script that prints a version banner was accepted as the plugin")
	}
	if !strings.Contains(err.Error(), "cannot be identified") {
		t.Errorf("the refusal must say the binary cannot be identified: %v", err)
	}
	if strings.Contains(err.Error(), "v0.0.0") {
		t.Errorf("the banner must not be treated as identity at all: %v", err)
	}
}

// TestOwnedPluginIsRebuiltFromCurrentSourcesEvenWhenPresent is F4's first counterexample: the
// repository's own redact plugin used to be accepted on a bare existence check, so editing
// pkg/localdeps sources changed nothing. The build script now runs for every generation, and the
// binary that comes out of it still has to be attributable to this repository.
func TestOwnedPluginIsRebuiltFromCurrentSourcesEvenWhenPresent(t *testing.T) {
	const stale = "#!/bin/sh\nprintf 'stale-plugin\\n'\n"

	setup := func(t *testing.T, buildExit string) (string, string) {
		t.Helper()
		root := t.TempDir()
		api := filepath.Join(root, "api")
		for _, d := range []string{api, filepath.Join(root, "scripts"), filepath.Dir(filepath.Join(root, "tools", "bin", "x"))} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(root, "tools", "bin", "protoc-gen-go-redact")
		if err := os.WriteFile(target, []byte(stale), 0o755); err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\ntouch \"" + filepath.Join(root, "rebuild-was-called") + "\"\nexit " + buildExit + "\n"
		if err := os.WriteFile(filepath.Join(root, "scripts", "build-redact-plugin.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return root, target
	}

	t.Run("an existing binary no longer short-circuits the build", func(t *testing.T) {
		root, target := setup(t, "0")
		v := &pluginVerifier{root: root, apiPath: filepath.Join(root, "api"), module: "go-wind-admin",
			verifiedPaths: map[string]string{}, filePlugins: map[string]error{}}
		err := v.resolve(context.Background(), pluginRef{Kind: "file", Program: "../tools/bin/protoc-gen-go-redact"})
		if err == nil {
			t.Fatal("a stale shell script was accepted as the plugin")
		}
		if _, err := os.Stat(filepath.Join(root, "rebuild-was-called")); err != nil {
			t.Fatalf("the build script was never invoked, so the binary's provenance is unproven: %v", err)
		}
		if !strings.Contains(err.Error(), "cannot be identified") {
			t.Errorf("presence must not substitute for identity: %v", err)
		}
		if got, _ := os.ReadFile(target); string(got) != stale {
			t.Errorf("a refused preflight must not replace the binary it could not verify: %q", got)
		}
	})

	t.Run("a failing build is a preflight failure", func(t *testing.T) {
		root, _ := setup(t, "91")
		v := &pluginVerifier{root: root, apiPath: filepath.Join(root, "api"), module: "go-wind-admin",
			verifiedPaths: map[string]string{}, filePlugins: map[string]error{}}
		err := v.resolve(context.Background(), pluginRef{Kind: "file", Program: "../tools/bin/protoc-gen-go-redact"})
		if err == nil || !strings.Contains(err.Error(), "building protoc-gen-go-redact failed") {
			t.Fatalf("the build script's exit status must surface, got %v", err)
		}
	})

	t.Run("the build runs once per generation, not once per template", func(t *testing.T) {
		root, _ := setup(t, "0")
		count := filepath.Join(root, "build-count")
		script := "#!/bin/sh\nprintf x >> \"" + count + "\"\nexit 1\n"
		if err := os.WriteFile(filepath.Join(root, "scripts", "build-redact-plugin.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		v := &pluginVerifier{root: root, apiPath: filepath.Join(root, "api"), module: "go-wind-admin",
			verifiedPaths: map[string]string{}, filePlugins: map[string]error{}}
		ref := pluginRef{Kind: "file", Program: "../tools/bin/protoc-gen-go-redact"}
		for i := 0; i < 4; i++ {
			if err := v.resolve(context.Background(), ref); err == nil {
				t.Fatal("expected the build to fail")
			}
		}
		raw, err := os.ReadFile(count)
		if err != nil {
			t.Fatalf("the build script never ran: %v", err)
		}
		if len(raw) != 1 {
			t.Errorf("the plugin was built %d times for one generation; the Go build cache makes repeats pure cost", len(raw))
		}
	})
}

// TestToolLockCoversEveryNamedPlugin reads the repository's own templates: every plugin the chain
// calls by PATH name has to have a lock entry, so a template cannot quietly start relying on an
// unpinned tool. This check does not run the tools, so it holds in any environment.
func TestToolLockCoversEveryNamedPlugin(t *testing.T) {
	root := repoRoot(t)
	api := filepath.Join(root, "api")
	lock, err := loadToolLock(root)
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, name := range activeGenConfigs {
		got, err := parseGenTemplate(filepath.Join(api, name))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, ref := range got.Plugins {
			if ref.Kind == "path" {
				named[ref.Program] = true
				if _, ok := lock[ref.Program]; !ok {
					t.Errorf("%s calls %q by name but %s has no entry for it", name, ref.Program, toolLockRel)
				}
			}
		}
	}
	if len(named) < 4 {
		t.Fatalf("only %d named plugins found across the chain; the parser is missing template entries", len(named))
	}
	if _, ok := lock["buf"]; !ok {
		t.Error("buf itself must be pinned in the tool lock")
	}
	t.Logf("%d named plugins and buf are all pinned in %s", len(named), toolLockRel)
}

// TestToolLockRejectsADamagedFile keeps a broken lock from silently becoming "no requirements".
func TestToolLockRejectsADamagedFile(t *testing.T) {
	cases := map[string]string{
		"no tools":         `{"tools":{}}`,
		"missing version":  `{"tools":{"protoc-gen-go":{"module":"google.golang.org/protobuf/cmd/protoc-gen-go"}}}`,
		"missing module":   `{"tools":{"protoc-gen-go":{"version":"v1.36.11"}}}`,
		"not an object":    `{"tools":[]}`,
		"corrupt json":     `{"tools":{`,
		"empty document":   ``,
		"wrong json types": `{"tools":{"protoc-gen-go":{"module":1,"version":2}}}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, filepath.FromSlash(toolLockRel))
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dir, []byte(doc), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadToolLock(root); err == nil {
				t.Fatalf("%s was accepted as a tool lock: %s", name, doc)
			}
		})
	}
	t.Run("a missing lock file is a failure", func(t *testing.T) {
		if _, err := loadToolLock(t.TempDir()); err == nil {
			t.Fatal("no lock file must not mean no version requirements")
		}
	})
}

// TestLoadToolLockReadsTheRealFile pins the shape the preflight depends on.
func TestLoadToolLockReadsTheRealFile(t *testing.T) {
	lock, err := loadToolLock(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(toolLockRel)))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Tools map[string]map[string]string `json:"tools"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Tools) != len(lock) {
		t.Fatalf("the lock has %d tools, %d were loaded", len(doc.Tools), len(lock))
	}
	for name, entry := range doc.Tools {
		if lock[name].Module != entry["module"] || lock[name].Version != entry["version"] {
			t.Errorf("%s: loaded %v, the file says %v", name, lock[name], entry)
		}
		if strings.Contains(entry["version"], "latest") {
			t.Errorf("%s is pinned to a floating version: %s", name, entry["version"])
		}
		if strings.HasPrefix(entry["module"], "github.com/tx7do/") {
			t.Errorf("%s is still pinned to an upstream tx7do module: %s", name, entry["module"])
		}
	}
}
