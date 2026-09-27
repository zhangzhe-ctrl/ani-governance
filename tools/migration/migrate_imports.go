// migrate_imports is a standard-library-only migration assistant.
// It does not fetch dependencies, run project tests, or change go.mod.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Import struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
	Line  int    `json:"line"`
}
type Method struct {
	Name    string `json:"name"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line"`
	Hash    string `json:"hash"`
}
type File struct {
	Path          string   `json:"path"`
	SHA256        string   `json:"sha256"`
	Generated     bool     `json:"generated"`
	Test          bool     `json:"test"`
	Imports       []Import `json:"imports"`
	Methods       []Method `json:"methods"`
	NonImportHash string   `json:"non_import_hash"`
	DirectiveHash string   `json:"directive_hash"`
}

func die(e error) {
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func printNode(n any) []byte {
	var b bytes.Buffer
	die(printer.Fprint(&b, token.NewFileSet(), n))
	return b.Bytes()
}
func directiveHash(b []byte) string {
	// Build/embed/compiler directives affect semantics but are not AST nodes.
	var lines []string
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//go:") || strings.HasPrefix(t, "// +build") || strings.HasPrefix(t, "//line ") {
			lines = append(lines, t)
		}
	}
	return digest([]byte(strings.Join(lines, "\n")))
}
func inspect(path string, b []byte) (File, error) {
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, path, b, parser.AllErrors)
	if e != nil {
		return File{}, e
	}
	out := File{Path: path, SHA256: digest(b), Generated: bytes.Contains(b, []byte("Code generated")), Test: strings.HasSuffix(path, "_test.go"), DirectiveHash: directiveHash(b)}
	for _, i := range f.Imports {
		p, e := strconv.Unquote(i.Path.Value)
		if e != nil {
			return File{}, e
		}
		x := Import{Path: p, Line: fs.Position(i.Pos()).Line}
		if i.Name != nil {
			x.Alias = i.Name.Name
		}
		out.Imports = append(out.Imports, x)
	}
	decls := make([]ast.Decl, 0, len(f.Decls))
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		decls = append(decls, d)
		if x, ok := d.(*ast.FuncDecl); ok {
			name := x.Name.Name
			if x.Recv != nil && len(x.Recv.List) > 0 {
				name = string(printNode(x.Recv.List[0].Type)) + "." + name
			}
			out.Methods = append(out.Methods, Method{name, fs.Position(x.Pos()).Line, fs.Position(x.End()).Line, digest(printNode(x))})
		}
	}
	f.Decls = decls
	f.Imports = nil
	out.NonImportHash = digest(printNode(f))
	return out, nil
}
func safe(root, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("expected repository-relative path: %q", path)
	}
	p := filepath.Clean(filepath.FromSlash(path))
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", path)
	}
	abs := filepath.Join(root, p)
	resolved, e := filepath.EvalSymlinks(abs)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, resolved)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("symlink escapes root: %q", path)
	}
	st, e := os.Lstat(abs)
	if e != nil {
		return "", e
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("not regular file: %q", path)
	}
	return abs, nil
}
func names(root, list string) ([]string, error) {
	var ns []string
	if list != "" {
		b, e := os.ReadFile(list)
		if e != nil {
			return nil, e
		}
		for _, p := range strings.Split(string(b), "\n") {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, "#") {
				ns = append(ns, p)
			}
		}
	} else {
		c := exec.Command("git", "-C", root, "ls-files", "-z", "--", "*.go")
		b, e := c.Output()
		if e != nil {
			return nil, fmt.Errorf("git ls-files: %w", e)
		}
		for _, p := range strings.Split(string(b), "\x00") {
			if p != "" && !strings.HasPrefix(p, "third_party/") && !strings.HasPrefix(p, "vendor/") && !strings.HasPrefix(p, "_tx7do_stage/") {
				ns = append(ns, p)
			}
		}
	}
	sort.Strings(ns)
	seen := map[string]bool{}
	out := []string{}
	for _, n := range ns {
		if !strings.HasSuffix(n, ".go") {
			return nil, fmt.Errorf("not Go file: %s", n)
		}
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	return out, nil
}
func main() {
	if len(os.Args) < 2 {
		die(fmt.Errorf("usage: migrate_imports scan|rewrite|guard|equiv [flags]"))
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	rootArg := fs.String("root", ".", "repository root")
	list := fs.String("files", "", "explicit relative Go file list; mandatory for rewrite/guard")
	outPath := fs.String("out", "", "JSON output, default stdout")
	mapPath := fs.String("map", "", "JSON old package/module prefix -> new prefix")
	write := fs.Bool("write", false, "apply imports only (default dry-run)")
	base := fs.String("base", "", "baseline git commit, guard only")
	before := fs.String("before", "", "original file, equiv only")
	after := fs.String("after", "", "copied/rebased file, equiv only")
	die(fs.Parse(os.Args[2:]))
	root, e := filepath.Abs(*rootArg)
	die(e)
	root, e = filepath.EvalSymlinks(root)
	die(e)
	if cmd == "equiv" {
		b, e := os.ReadFile(*before)
		die(e)
		a, e := os.ReadFile(*after)
		die(e)
		bi, e := inspect(*before, b)
		die(e)
		ai, e := inspect(*after, a)
		die(e)
		if bi.NonImportHash != ai.NonImportHash || bi.DirectiveHash != ai.DirectiveHash {
			die(fmt.Errorf("non-import AST changed: %s -> %s", *before, *after))
		}
		fmt.Println("PASS: non-import AST and directives equivalent (not a runtime equivalence proof)")
		return
	}
	if cmd != "scan" && cmd != "rewrite" && cmd != "guard" {
		die(fmt.Errorf("unknown command %q", cmd))
	}
	if cmd != "scan" && *list == "" {
		die(fmt.Errorf("explicit -files is required"))
	}
	ns, e := names(root, *list)
	die(e)
	if len(ns) == 0 {
		die(fmt.Errorf("no files selected"))
	}
	mapping := map[string]string{}
	if cmd == "rewrite" {
		b, e := os.ReadFile(*mapPath)
		die(e)
		die(json.Unmarshal(b, &mapping))
		for k, v := range mapping {
			if !strings.HasPrefix(k, "github.com/tx7do/") || !strings.HasPrefix(v, "go-wind-admin/") {
				die(fmt.Errorf("unexpected mapping %q -> %q", k, v))
			}
		}
	}
	keys := []string{}
	for k := range mapping {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	results := []File{}
	changes := []string{}
	type planned struct {
		path string
		b    []byte
		mode os.FileMode
	}
	plans := []planned{}
	for _, n := range ns {
		p, e := safe(root, n)
		die(e)
		b, e := os.ReadFile(p)
		die(e)
		info, e := inspect(n, b)
		die(e)
		switch cmd {
		case "scan":
			results = append(results, info)
		case "guard":
			if *base == "" {
				die(fmt.Errorf("-base required"))
			}
			c := exec.Command("git", "-C", root, "show", *base+":"+n)
			old, e := c.Output()
			if e != nil {
				die(fmt.Errorf("%s unavailable at base; verify new files with equiv and provenance instead", n))
			}
			prev, e := inspect(n, old)
			die(e)
			if prev.NonImportHash != info.NonImportHash || prev.DirectiveHash != info.DirectiveHash {
				die(fmt.Errorf("NON-IMPORT CHANGE: %s (includes function bodies, types and constants)", n))
			}
		case "rewrite":
			fset := token.NewFileSet()
			f, e := parser.ParseFile(fset, n, b, parser.AllErrors)
			die(e)
			type patch struct {
				s, e int
				v    string
			}
			ps := []patch{}
			for _, imp := range f.Imports {
				old, e := strconv.Unquote(imp.Path.Value)
				die(e)
				for _, k := range keys {
					if old == k || strings.HasPrefix(old, k+"/") {
						v := mapping[k] + strings.TrimPrefix(old, k)
						if old != v {
							ps = append(ps, patch{fset.Position(imp.Path.Pos()).Offset, fset.Position(imp.Path.End()).Offset, strconv.Quote(v)})
						}
						break
					}
				}
			}
			if len(ps) == 0 {
				continue
			}
			sort.Slice(ps, func(i, j int) bool { return ps[i].s > ps[j].s })
			modified := append([]byte(nil), b...)
			for _, x := range ps {
				modified = append(modified[:x.s], append([]byte(x.v), modified[x.e:]...)...)
			}
			afterInfo, e := inspect(n, modified)
			die(e)
			if info.NonImportHash != afterInfo.NonImportHash || info.DirectiveHash != afterInfo.DirectiveHash {
				die(fmt.Errorf("internal safety check failed: %s", n))
			}
			changes = append(changes, n)
			st, e := os.Stat(p)
			die(e)
			plans = append(plans, planned{p, modified, st.Mode()})
		}
	}
	// All inputs parse and validate before writing. Files are replaced individually;
	// a failed process is NOT a transaction for the whole working tree.
	if cmd == "rewrite" && *write {
		for _, p := range plans {
			f, e := os.CreateTemp(filepath.Dir(p.path), ".import-rebase-*")
			die(e)
			tmp := f.Name()
			die(f.Chmod(p.mode.Perm()))
			_, e = f.Write(p.b)
			if e != nil {
				f.Close()
				os.Remove(tmp)
				die(e)
			}
			die(f.Close())
			e = os.Rename(tmp, p.path)
			if e != nil {
				os.Remove(tmp)
				die(e)
			}
		}
	}
	var value any = results
	if cmd == "rewrite" {
		value = map[string]any{"dry_run": !*write, "changed_files": changes, "count": len(changes)}
	}
	if cmd == "guard" {
		value = map[string]any{"pass": true, "checked_files": len(ns), "scope": "non-import AST and Go directives, not runtime behavior"}
	}
	b, e := json.MarshalIndent(value, "", "  ")
	die(e)
	b = append(b, '\n')
	if *outPath == "" {
		_, e = os.Stdout.Write(b)
		die(e)
	} else {
		die(os.WriteFile(*outPath, b, 0644))
	}
}
