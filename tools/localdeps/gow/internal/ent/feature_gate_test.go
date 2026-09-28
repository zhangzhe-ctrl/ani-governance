package ent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestGenerateFeaturesMatchContract pins the accepted output-producing options.
// The Make command stub separately checks that service/root/CI call this gow path.
func TestGenerateFeaturesMatchContract(t *testing.T) {
	want := []string{"entql", "privacy", "sql/lock", "sql/modifier", "sql/upsert"}
	match := func(args []string) bool {
		got := featureValues(t, strings.Join(args, " "))
		return strings.Join(want, ",") == strings.Join(got, ",") && len(args) == 2*len(want)
	}
	if !match(entGenerateFeatures) {
		t.Fatalf("unexpected Ent feature set: %v", entGenerateFeatures)
	}
	if match(append(append([]string{}, entGenerateFeatures...), "--feature", "sql/versioned-migration")) {
		t.Fatal("an extra feature must be rejected")
	}
	if match(entGenerateFeatures[:len(entGenerateFeatures)-2]) {
		t.Fatal("a missing feature must be rejected")
	}
}

// TestGenerateFeaturesExcludeVersionedMigration names the one feature that must not come back:
// upstream gowind@v1.0.3 passes it for its own gow migrate --versioned, which this repository
// did not take over, and it adds a 32-line Diff/NamedDiff block to ent/migrate/migrate.go.
func TestGenerateFeaturesExcludeVersionedMigration(t *testing.T) {
	if strings.Contains(strings.Join(entGenerateFeatures, " "), "sql/versioned-migration") {
		t.Fatal("sql/versioned-migration must stay out of the accepted Ent tree")
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
