package rpc

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestWhiteListBasic(t *testing.T) {
	// ensure clean state
	ClearWhiteList()

	AddWhiteList("Health.Check", "Public.Ping")

	if !DefaultWhiteList.IsWhitelisted("Health.Check") {
		t.Fatalf("expected Health.Check to be whitelisted")
	}
	if !DefaultWhiteList.IsWhitelisted("Public.Ping") {
		t.Fatalf("expected Public.Ping to be whitelisted")
	}
	// unknown should not be whitelisted
	if DefaultWhiteList.IsWhitelisted("Other.Op") {
		t.Fatalf("expected Other.Op NOT to be whitelisted")
	}

	// MatchFunc should return false (skip middleware) when whitelisted
	m := NewWhiteListMatcher()
	if m(context.Background(), "Health.Check") {
		t.Fatalf("expected matcher to be false for whitelisted op")
	}
	if !m(context.Background(), "Other.Op") {
		t.Fatalf("expected matcher to be true for non-whitelisted op")
	}

	ClearWhiteList()
	if DefaultWhiteList.IsWhitelisted("Health.Check") {
		t.Fatalf("expected Health.Check to be cleared")
	}
}

func TestWhiteListNormalization(t *testing.T) {
	// DefaultWhiteList is shared with the other tests in this file, so restore
	// whatever this test perturbs even when an assertion fails, and never run
	// this test in parallel.
	saved := DefaultWhiteList.Snapshot()
	t.Cleanup(func() { SetWhiteList(saved) })

	const registered = "/pkg.Service/MethodX"

	// An entry registered with its leading slash is stored with the slash
	// trimmed. The full operation matches in either spelling; a bare method name
	// does not, and neither does the same method under another service.
	exact := NewWhiteList(Exact, registered)
	for _, c := range []struct {
		op   string
		want bool
	}{
		{"/pkg.Service/MethodX", true},
		{"pkg.Service/MethodX", true},
		{"MethodX", false},
		{"pkg.Service/MethodY", false},
		{"pkg.Other/MethodX", false},
		{"pkg.Service", false},
		{"", false},
	} {
		if got := exact.IsWhitelisted(c.op); got != c.want {
			t.Errorf("IsWhitelisted(%q) = %v, want %v (entry %q)", c.op, got, c.want, registered)
		}
	}

	// The method-only fallback exists only for an explicitly registered bare
	// method. A separate instance keeps that difference observable.
	bare := NewWhiteList(Exact, "MethodX")
	for _, c := range []struct {
		op   string
		want bool
	}{
		{"MethodX", true},
		{"/pkg.Service/MethodX", true},
		{"pkg.Service/MethodX", true},
		{"pkg.Service/MethodY", false},
	} {
		if got := bare.IsWhitelisted(c.op); got != c.want {
			t.Errorf("fallback IsWhitelisted(%q) = %v, want %v", c.op, got, c.want)
		}
	}

	// MatchFunc reports whether middleware must run: a whitelisted operation
	// skips it, and a protected operation that merely shares a method name with
	// an entry still goes through it.
	m := exact.MatchFunc()
	ctx := context.Background()
	for _, c := range []struct {
		op   string
		want bool
	}{
		{"pkg.Service/MethodX", false},
		{"/pkg.Service/MethodX", false},
		{"pkg.Other/MethodX", true},
		{"pkg.Service/MethodZ", true},
		{"", true},
	} {
		if got := m(ctx, c.op); got != c.want {
			t.Errorf("MatchFunc(%q) = %v, want %v", c.op, got, c.want)
		}
	}

	// The same contract holds through the package-level API, and Clear is visible.
	ClearWhiteList()
	AddWhiteList(registered)
	if !DefaultWhiteList.IsWhitelisted("pkg.Service/MethodX") {
		t.Errorf("the package-level list lost the entry %q", registered)
	}
	if DefaultWhiteList.IsWhitelisted("MethodX") {
		t.Errorf("the package-level list matched a bare method with no explicit entry for it")
	}
	ClearWhiteList()
	if DefaultWhiteList.IsWhitelisted("pkg.Service/MethodX") {
		t.Errorf("Clear did not empty the package-level list")
	}
}

func TestWhiteListConcurrent(t *testing.T) {
	ClearWhiteList()
	SetWhiteList([]string{"A", "B", "C"})

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 200

	// readers
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = DefaultWhiteList.IsWhitelisted("A")
				_ = DefaultWhiteList.IsWhitelisted("NonExistent")
				_ = NewWhiteListMatcher()(context.Background(), "B")
			}
		}(i)
	}

	// writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				AddWhiteList("X", "Y")
				SetWhiteList([]string{"A", "Z"})
				ClearWhiteList()
				SetWhiteList([]string{"A", "B", "C"})
				// small sleep to increase interleaving
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
