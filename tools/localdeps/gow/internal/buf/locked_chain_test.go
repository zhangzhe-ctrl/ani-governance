package buf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = up
	}
}

// TestParseGenTemplateMatchesTheAcceptedTemplates reads the repository's own templates and pins
// what the preflight depends on: which plugin versions each entry names, which output roots exist
// and that only the main template is allowed to clear anything.
func TestParseGenTemplateMatchesTheAcceptedTemplates(t *testing.T) {
	root := repoRoot(t)
	api := filepath.Join(root, "api")

	cases := []struct {
		name     string
		clean    bool
		outs     []string
		wantPin  string
		inputDir string
	}{
		{"buf.gen.yaml", true, []string{"gen/go"}, "", "protos"},
		// The scoped PGV entry runs the PATH plugin, whose version is pinned by make plugin and
		// recorded in the tool lock; it is deliberately not a `go run` reference.
		{"buf.validate.gen.yaml", false, []string{"gen/go"}, "", "protos"},
		{"buf.aksk.gen.yaml", false, []string{"gen/go"}, "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0", "protos"},
		{"buf.redact.gen.yaml", false, []string{"../pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact"}, "", "localdeps/redact"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseGenTemplate(filepath.Join(api, c.name))
			if err != nil {
				t.Fatalf("parse %s: %v", c.name, err)
			}
			if got.Clean != c.clean {
				t.Errorf("clean = %v, want %v", got.Clean, c.clean)
			}
			if len(got.Outs) == 0 {
				t.Fatal("no outputs parsed")
			}
			if !containsAll(got.Outs, c.outs) {
				t.Errorf("outs = %v, want to contain %v", got.Outs, c.outs)
			}
			if !contains(got.InputDirs, c.inputDir) {
				t.Errorf("input dirs = %v, want %s", got.InputDirs, c.inputDir)
			}
			if c.wantPin != "" {
				var pins []string
				for _, p := range got.Plugins {
					if m, ok := p.goRunModule(); ok {
						pins = append(pins, m)
					}
				}
				if !contains(pins, c.wantPin) {
					t.Errorf("pinned plugins = %v, want %s", pins, c.wantPin)
				}
			}
		})
	}

	val, err := parseGenTemplate(filepath.Join(api, "buf.validate.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(val.Plugins) != 1 || val.Plugins[0].Kind != "path" || val.Plugins[0].Program != "protoc-gen-validate" {
		t.Errorf("buf.validate.gen.yaml plugins = %+v, want one PATH protoc-gen-validate", val.Plugins)
	}

	// The redact plugin must be recognised as a file reference, not a PATH lookup: that is the
	// difference between "build it from this module" and "search the developer's GOBIN".
	main, err := parseGenTemplate(filepath.Join(api, "buf.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range main.Plugins {
		if p.Kind == "file" && strings.HasSuffix(p.Program, "protoc-gen-go-redact") {
			found = true
		}
	}
	if !found {
		t.Errorf("buf.gen.yaml redact plugin not classified as a file reference: %+v", main.Plugins)
	}

	// The scoped PGV entry and the versioned scope ledger must carry the same file count, or one
	// of them has drifted without the other noticing.
	raw, err := os.ReadFile(filepath.Join(api, "buf.validate.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_ = raw
	scopeDoc, err := os.ReadFile(filepath.Join(root, "migration", "pgv-scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scope struct {
		Entries []struct {
			Validator string `json:"validator"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(scopeDoc, &scope); err != nil {
		t.Fatal(err)
	}
	if len(main.InputPaths) > 0 {
		t.Fatalf("the main template must not carry per-file paths: %d", len(main.InputPaths))
	}
	v, err := parseGenTemplate(filepath.Join(api, "buf.validate.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.InputPaths) != len(scope.Entries) {
		t.Fatalf("template paths (%d) and pgv-scope entries (%d) disagree", len(v.InputPaths), len(scope.Entries))
	}
}

func TestDeriveManagedRootsStaysInsideTheRepository(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	if err := os.MkdirAll(api, 0o755); err != nil {
		t.Fatal(err)
	}
	escape := &genTemplate{Name: "bad.yaml", Outs: []string{"../../etc"}}
	if _, err := deriveManagedRoots(root, api, []*genTemplate{escape}); err == nil {
		t.Fatal("an out: that escapes the module root must be refused")
	}
	inside := &genTemplate{Name: "ok.yaml", Outs: []string{"gen/go", "../pkg/localdeps/x"}}
	roots, err := deriveManagedRoots(root, api, []*genTemplate{inside})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range roots {
		got = append(got, r.dir)
	}
	want := []string{filepath.Join("api", "gen", "go"), filepath.Join("pkg", "localdeps", "x")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v, want %v", got, want)
	}
}

// TestPreflightFailsWithoutTouchingOutputs is the regression for the destructive case measured in
// T15: a clean checkout where a plugin is missing must fail while every committed file stands.
func TestPreflightFailsWithoutTouchingOutputs(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	out := filepath.Join(api, "gen", "go")
	if err := os.MkdirAll(filepath.Join(api, "protos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(out, "committed.go")
	if err := os.WriteFile(keep, []byte("package v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"buf.yaml", "buf.lock"} {
		if err := os.WriteFile(filepath.Join(api, name), []byte("version: v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	bad := &genTemplate{
		Name:      "buf.missing.gen.yaml",
		Path:      filepath.Join(api, "buf.missing.gen.yaml"),
		Clean:     true,
		Plugins:   []pluginRef{{Kind: "path", Program: "protoc-gen-this-does-not-exist", Raw: "protoc-gen-this-does-not-exist"}},
		InputDirs: []string{"protos"},
		Outs:      []string{"gen/go"},
	}
	err := preflightGenerate(context.Background(), root, api, []*genTemplate{bad})
	if err == nil {
		t.Fatal("preflight must fail when a plugin is absent")
	}
	if !strings.Contains(err.Error(), "no generated file was touched") {
		t.Errorf("unexpected error: %v", err)
	}
	sum, err := fileSum(keep)
	if err != nil || sum == "" {
		t.Fatalf("committed output must survive preflight: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "bin")); err == nil {
		t.Error("preflight must not create output directories")
	}
}

// TestSyncBackRefusesLossAndUnapprovedValidators covers the two ways a generation result could be
// quietly wrong: an artifact the chain stopped producing, and a validator nobody approved.
func TestSyncBackRefusesLossAndUnapprovedValidators(t *testing.T) {
	root := t.TempDir()
	stage := t.TempDir()
	managed := filepath.Join("api", "gen", "go")
	for _, base := range []string{root, stage} {
		if err := os.MkdirAll(filepath.Join(base, managed, "v1"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, managed, "v1", "a.pb.go"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, managed, "v1", "a.pb.go"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope := map[string]any{"entries": []map[string]string{
		{"validator": filepath.Join(managed, "v1", "approved.pb.validate.go")},
	}}
	buf, _ := json.Marshal(scope)
	if err := os.MkdirAll(filepath.Join(root, "migration"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "migration", "pgv-scope.json"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
	roots := []managedRoot{{dir: managed, name: "buf.gen.yaml"}}

	// 1. staged output lost a committed file -> refuse, leave the tree alone
	if err := os.Remove(filepath.Join(stage, managed, "v1", "a.pb.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, managed, "v1", "b.pb.go"), []byte("only-in-stage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncBack(root, stage, roots); err == nil {
		t.Fatal("a lost managed file must fail the sync")
	}
	if got, _ := os.ReadFile(filepath.Join(root, managed, "v1", "a.pb.go")); string(got) != "old\n" {
		t.Errorf("the tree was modified despite the refusal: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, managed, "v1", "b.pb.go")); err == nil {
		t.Error("nothing may be written back when the comparison fails")
	}

	// 2. an unapproved validator -> refuse
	if err := os.WriteFile(filepath.Join(stage, managed, "v1", "a.pb.go"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, managed, "v1", "sneak.pb.validate.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncBack(root, stage, roots); err == nil {
		t.Fatal("a validator outside migration/pgv-scope.json must fail the sync")
	}
	if _, err := os.Stat(filepath.Join(root, managed, "v1", "sneak.pb.validate.go")); err == nil {
		t.Error("an unapproved validator must not be written into the tree")
	}

	// 3. approved validator plus a changed file -> both come across
	if err := os.Remove(filepath.Join(stage, managed, "v1", "sneak.pb.validate.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, managed, "v1", "approved.pb.validate.go"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncBack(root, stage, roots); err != nil {
		t.Fatalf("sync of approved output: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, managed, "v1", "a.pb.go")); string(got) != "new\n" {
		t.Errorf("changed file not written back: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, managed, "v1", "approved.pb.validate.go")); string(got) != "ok\n" {
		t.Errorf("approved validator not written back: %q", got)
	}
}

func TestEscapeModulePathMatchesTheCacheEncoding(t *testing.T) {
	for _, c := range map[string]string{
		"github.com/Getkin/kin-openapi":                 "github.com/!getkin/kin-openapi",
		"github.com/go-kratos/kratos":                   "github.com/go-kratos/kratos",
		"google.golang.org/grpc/cmd/protoc-gen-go-grpc": "google.golang.org/grpc/cmd/protoc-gen-go-grpc",
	} {
		if got := escapeModulePath(c); !strings.EqualFold(strings.ReplaceAll(got, "!", ""), strings.ReplaceAll(c, "!", "")) {
			t.Errorf("escapeModulePath(%q) = %q changed letters", c, got)
		}
	}
	if got := escapeModulePath("github.com/DataDog/zstd"); got != "github.com/!data!dog/zstd" {
		t.Errorf("escapeModulePath = %q, want github.com/!data!dog/zstd", got)
	}
	// A real pin used by the templates must resolve in this machine's module cache, which is what
	// the preflight relies on to fail early instead of mid-generation.
	// No skip here on purpose: this is the exact check the preflight performs before generation,
	// so a missing cache entry has to be reported as a failure rather than silently passed over.
	cache, err := goModuleCache(context.Background())
	if err != nil {
		t.Fatalf("goModuleCache: %v", err)
	}
	if err := verifyGoRunModule(cache, "github.com/envoyproxy/protoc-gen-validate@v1.3.3"); err != nil {
		t.Fatalf("verifyGoRunModule: %v (this is what preflight would report before generation)", err)
	}
}

// TestEveryTemplatePinResolvesOffline walks the same active template list the chain runs and
// requires every `go run pkg@version` plugin it names to be present locally. Preflight performs
// this before any output is written, so a missing pin has to be visible here as a failure rather
// than discovered half way through a generation that already cleared an output root.
func TestEveryTemplatePinResolvesOffline(t *testing.T) {
	root := repoRoot(t)
	api := filepath.Join(root, "api")
	cache, err := goModuleCache(context.Background())
	if err != nil {
		t.Fatalf("goModuleCache: %v", err)
	}
	pins := map[string]bool{}
	for _, name := range activeGenConfigs {
		got, err := parseGenTemplate(filepath.Join(api, name))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, ref := range got.Plugins {
			mod, ok := ref.goRunModule()
			if !ok {
				continue
			}
			pins[mod] = true
			if err := verifyGoRunModule(cache, mod); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
	if len(pins) < 4 {
		t.Fatalf("only %d pinned `go run` plugins found; the parser is missing template entries", len(pins))
	}
	t.Logf("resolved %d distinct pinned plugins offline", len(pins))
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func containsAll(hay, needles []string) bool {
	for _, n := range needles {
		if !contains(hay, n) {
			return false
		}
	}
	return true
}

// TestStagingInputsCarryEveryStepTheChainRuns keeps the staged copy complete: a script the chain
// runs but never stages would make the run fail in a way that only shows up on a clean checkout.
func TestStagingInputsCarryEveryStepTheChainRuns(t *testing.T) {
	root := repoRoot(t)
	want := map[string]bool{"scripts/build-redact-plugin.sh": false, finalizeScriptRel: false}
	have := map[string]bool{}
	for _, in := range stagingInputs() {
		have[in] = true
	}
	for path := range want {
		if !have[path] {
			t.Errorf("stagingInputs() does not copy %s, which the chain runs", path)
		}
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("%s is missing from the repository: %v", path, err)
		}
	}
	for _, in := range stagingInputs() {
		if _, err := os.Stat(filepath.Join(root, in)); err != nil {
			t.Errorf("staged input %s does not exist: %v", in, err)
		}
	}
}

// TestFinalizeOpenAPIRunsInsideTheStagedCopy proves the tail step executes against the copy rather
// than the repository: the marker below can only appear under the stage directory.
func TestFinalizeOpenAPIRunsInsideTheStagedCopy(t *testing.T) {
	stage := t.TempDir()
	script := filepath.Join(stage, filepath.FromSlash(finalizeScriptRel))
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "import pathlib, sys\npathlib.Path('ran-inside-stage').write_text('ok')\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := finalizeOpenAPI(context.Background(), stage); err != nil {
		t.Fatalf("finalizeOpenAPI: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(stage, "ran-inside-stage"))
	if err != nil {
		t.Fatalf("the script did not run with the stage as its working directory: %v", err)
	}
	if string(got) != "ok" {
		t.Errorf("marker = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "ran-inside-stage")); err == nil {
		t.Fatal("the post-processing step wrote into the repository instead of the staged copy")
	}
	if err := finalizeOpenAPI(context.Background(), t.TempDir()); err == nil {
		t.Error("a staged copy without the post-processing script must fail rather than skip")
	}
}

// TestFinalizeOpenApiDocumentIsNotIdempotent records why the post-processing belongs inside the
// staged run: scripts/finalize-aksk-openapi.py removes one synthetic response and refuses to do it
// twice, so an outer make openapi after gow api would break an otherwise good build.
func TestFinalizeOpenApiDocumentIsNotIdempotent(t *testing.T) {
	root := repoRoot(t)
	stage := t.TempDir()
	target := filepath.Join(stage, filepath.FromSlash("app/admin/service/cmd/server/assets/openapi.yaml"))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "app/admin/service/cmd/server/assets/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, doc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(filepath.Join(root, filepath.FromSlash(finalizeScriptRel)),
		filepath.Join(stage, filepath.FromSlash(finalizeScriptRel))); err != nil {
		t.Fatal(err)
	}
	if err := finalizeOpenAPI(context.Background(), stage); err == nil {
		t.Fatal("running the post-processing over the already final document must fail loudly, not pass silently")
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(doc) {
		t.Error("the refused post-processing must leave the document byte-identical")
	}
}
