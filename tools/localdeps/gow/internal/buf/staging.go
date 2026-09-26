package buf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// managedRoot is one output directory a template writes, as a path relative to the module root.
type managedRoot struct {
	dir  string
	name string
}

// deriveManagedRoots turns every template's out: values into module-relative output roots and
// refuses anything that would write outside the repository.
func deriveManagedRoots(root, apiPath string, templates []*genTemplate) ([]managedRoot, error) {
	seen := map[string]bool{}
	var out []managedRoot
	for _, t := range templates {
		for _, o := range t.Outs {
			// Every out: is written relative to the api directory, including the ../ ones that
			// land in pkg/localdeps or the embedded assets.
			abs := filepath.Clean(filepath.Join(apiPath, o))
			rel, err := filepath.Rel(root, abs)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return nil, fmt.Errorf("%s writes outside the module root: %s", t.Name, o)
			}
			if !seen[rel] {
				seen[rel] = true
				out = append(out, managedRoot{dir: rel, name: t.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out, nil
}

// stageAndGenerate runs the accepted template walk against a throwaway copy of the inputs and
// only then brings managed files back. The point is that clean: true in api/buf.gen.yaml can
// clear a directory all it likes: it clears the copy, never api/gen/go or the output roots that
// hold hand-written runtime files, unless generation has already succeeded in full.
func stageAndGenerate(ctx context.Context, root string, templates []*genTemplate, roots []managedRoot) error {
	stage, err := os.MkdirTemp("", "gow-api-stage-")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			fmt.Fprintf(os.Stderr, "staging cleanup failed: %v\n", err)
		}
	}()

	for _, in := range []string{"go.mod", "go.sum", "api", "pkg"} {
		if err := copyTree(filepath.Join(root, in), filepath.Join(stage, in)); err != nil {
			return fmt.Errorf("staging %s: %w", in, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(stage, "tools", "bin"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(stage, "scripts"), 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "scripts", "build-redact-plugin.sh"), filepath.Join(stage, "scripts", "build-redact-plugin.sh")); err != nil {
		return fmt.Errorf("staging the redact build script: %w", err)
	}
	// Every managed output root has to exist in the copy with its current content, including the
	// ones outside api/ and pkg/. The openapi template writes into
	// app/admin/service/cmd/server/assets, a directory that also holds the hand-written
	// go:embed file assets.go: without this the comparison would call that hand-written file
	// "lost" whenever the chain did not actually touch it.
	for _, m := range roots {
		if strings.HasPrefix(m.dir, "api"+string(os.PathSeparator)) || strings.HasPrefix(m.dir, "pkg"+string(os.PathSeparator)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, m.dir)); err == nil {
			if err := copyTree(filepath.Join(root, m.dir), filepath.Join(stage, m.dir)); err != nil {
				return fmt.Errorf("staging %s: %w", m.dir, err)
			}
		}
	}

	plugin := filepath.Join(root, "tools", "bin", "protoc-gen-go-redact")
	if _, err := os.Stat(plugin); err == nil {
		if err := copyFile(plugin, filepath.Join(stage, "tools", "bin", "protoc-gen-go-redact")); err != nil {
			return fmt.Errorf("staging protoc-gen-go-redact: %w", err)
		}
	}

	stageAPI := filepath.Join(stage, "api")
	fmt.Printf("generating into staging copy %s (%d templates, %d managed output roots)\n", stage, len(templates), len(roots))
	for _, t := range templates {
		fmt.Printf("Using template file: %s\n", t.Path)
		if err := runBufGenerateIn(ctx, stageAPI, t.Name); err != nil {
			return err
		}
	}

	return syncBack(root, stage, roots)
}

// syncBack compares the staged output with the working tree and copies only what generation
// produced. A file that generation no longer produces is an error rather than a silent delete,
// and a new validator outside the approved scope is an error rather than a surprise in the diff.
func syncBack(root, stage string, roots []managedRoot) error {
	approved, err := approvedValidators(root)
	if err != nil {
		return err
	}

	var changed, added []string
	var missing, rejected []string

	stagedCount, treeCount := 0, map[string]int{}
	for _, m := range roots {
		staged, err := treeDigests(filepath.Join(stage, m.dir))
		if err != nil {
			return err
		}
		current, err := treeDigests(filepath.Join(root, m.dir))
		if err != nil {
			return err
		}
		treeCount[m.dir] = len(current)
		stagedCount += len(staged)
		for rel, sum := range staged {
			if old, ok := current[rel]; !ok {
				if approved != nil && strings.HasSuffix(rel, ".pb.validate.go") && !approved[filepath.Join(m.dir, rel)] {
					rejected = append(rejected, filepath.Join(m.dir, rel))
					continue
				}
				added = append(added, filepath.Join(m.dir, rel))
			} else if old != sum {
				changed = append(changed, filepath.Join(m.dir, rel))
			}
		}
		for rel := range current {
			if _, ok := staged[rel]; !ok {
				missing = append(missing, filepath.Join(m.dir, rel))
			}
		}
	}
	sort.Strings(changed)
	sort.Strings(added)
	sort.Strings(missing)
	sort.Strings(rejected)

	if len(rejected) > 0 {
		for _, f := range rejected {
			_, _ = fmt.Fprintf(os.Stderr, "outside the approved validator scope: %s\n", f)
		}
		return fmt.Errorf("generation produced %d validator(s) outside migration/pgv-scope.json; nothing was written back", len(rejected))
	}
	if len(missing) > 0 {
		for _, f := range missing {
			_, _ = fmt.Fprintf(os.Stderr, "no longer produced by the chain: %s\n", f)
		}
		return fmt.Errorf("generation lost %d managed file(s); the working tree was left unchanged", len(missing))
	}

	for _, rel := range append(append([]string{}, changed...), added...) {
		if err := copyFile(filepath.Join(stage, rel), filepath.Join(root, rel)); err != nil {
			return fmt.Errorf("writing back %s: %w", rel, err)
		}
	}
	detail := make([]string, 0, len(treeCount))
	for k, v := range treeCount {
		detail = append(detail, fmt.Sprintf("%s:%d", k, v))
	}
	sort.Strings(detail)
	fmt.Printf("staged generation ok: %d updated, %d new, %d staged files under %d managed roots [%s]\n",
		len(changed), len(added), stagedCount, len(roots), strings.Join(detail, " "))
	for _, rel := range changed {
		fmt.Printf("  updated: %s\n", rel)
	}
	for _, rel := range added {
		fmt.Printf("  new:     %s\n", rel)
	}
	return nil
}

// approvedValidators returns the committed PGV scope as a set, or nil when no scope file exists
// (the scope belongs to this repository's api tree, so other callers simply skip the rule).
func approvedValidators(root string) (map[string]bool, error) {
	path := filepath.Join(root, "migration", "pgv-scope.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Entries []struct {
			Validator string `json:"validator"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	set := make(map[string]bool, len(doc.Entries))
	for _, e := range doc.Entries {
		set[e.Validator] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("%s lists no validators", path)
	}
	return set, nil
}

func treeDigests(dir string) (map[string]string, error) {
	out := map[string]string{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum, err := fileSum(path)
		if err != nil {
			return err
		}
		out[rel] = sum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}
	return out, nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".copy-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst)
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}
