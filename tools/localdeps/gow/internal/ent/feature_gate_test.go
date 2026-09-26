package ent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestGenerateFeaturesMatchEntGate keeps gow ent admin and the Ent gate of make verify-gpu on
// one generation contract. The gate is scripts/verify-quota-schema.sh: it regenerates the whole
// ent tree and diffs it against the committed files, so any feature this tool adds or drops
// either breaks the gate or narrows what the gate can still reproduce.
func TestGenerateFeaturesMatchEntGate(t *testing.T) {
	gate := filepath.Join(repoRoot(t), "scripts", "verify-quota-schema.sh")
	src, err := os.ReadFile(gate)
	if err != nil {
		t.Fatalf("read the Ent gate script %s: %v", gate, err)
	}

	want := featureValues(t, string(src))
	got := featureValues(t, strings.Join(entGenerateFeatures, " "))

	if len(want) == 0 {
		t.Fatal("the gate script exposes no --feature list; the comparison would prove nothing")
	}
	if strings.Join(want, ",") != strings.Join(got, ",") {
		t.Fatalf("ent features diverged from the gate\n gate: %s\n gow : %s", strings.Join(want, ","), strings.Join(got, ","))
	}
}

// TestGenerateFeaturesExcludeVersionedMigration names the one feature that must not come back:
// upstream gowind@v1.0.3 passes it for its own gow migrate --versioned, which this repository
// did not take over, and it adds a 32-line Diff/NamedDiff block to ent/migrate/migrate.go.
func TestGenerateFeaturesExcludeVersionedMigration(t *testing.T) {
	if strings.Contains(strings.Join(entGenerateFeatures, " "), "sql/versioned-migration") {
		t.Fatal("sql/versioned-migration must stay out: the accepted ent tree and verify-quota-ent are generated without it")
	}
}

func featureValues(t *testing.T, text string) []string {
	t.Helper()
	matches := regexp.MustCompile(`--feature[= ]([A-Za-z0-9/_-]+)`).FindAllStringSubmatch(text, -1)
	values := make([]string, 0, len(matches))
	for _, m := range matches {
		values = append(values, m[1])
	}
	sort.Strings(values)
	return values
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
