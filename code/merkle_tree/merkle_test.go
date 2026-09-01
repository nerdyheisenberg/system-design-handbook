package main

import (
	"fmt"
	"reflect"
	"testing"
)

func dataset(n int) map[string]string {
	m := make(map[string]string, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("key:%06d", i)] = fmt.Sprintf("value:%d", i)
	}
	return m
}

// The core property: identical data anywhere must produce an identical root, so
// one hash comparison settles "are these replicas in sync?".
func TestIdenticalDataSameRoot(t *testing.T) {
	a := Build(dataset(1000))
	b := Build(dataset(1000))

	if a.RootHash() != b.RootHash() {
		t.Error("identical datasets produced different roots")
	}
	if len(Diff(a, b)) != 0 {
		t.Error("identical datasets reported differences")
	}
}

// Map iteration order is random in Go, so this would fail without sorting keys.
func TestRootIsIndependentOfInsertionOrder(t *testing.T) {
	first := Build(dataset(500)).RootHash()
	for i := 0; i < 20; i++ {
		if got := Build(dataset(500)).RootHash(); got != first {
			t.Fatalf("root varied across builds: %s vs %s", first, got)
		}
	}
}

func TestSingleValueChangeChangesRoot(t *testing.T) {
	base := dataset(1000)
	a := Build(base)

	modified := dataset(1000)
	modified["key:000500"] = "different"
	b := Build(modified)

	if a.RootHash() == b.RootHash() {
		t.Fatal("changing one value did not change the root")
	}
	if got := Diff(a, b); !reflect.DeepEqual(got, []string{"key:000500"}) {
		t.Errorf("Diff = %v, want exactly [key:000500]", got)
	}
}

func TestDetectsMultipleDifferences(t *testing.T) {
	a := Build(dataset(10000))

	modified := dataset(10000)
	modified["key:000042"] = "x"
	modified["key:005000"] = "y"
	modified["key:009999"] = "z"
	b := Build(modified)

	want := []string{"key:000042", "key:005000", "key:009999"}
	if got := Diff(a, b); !reflect.DeepEqual(got, want) {
		t.Errorf("Diff = %v, want %v", got, want)
	}
}

func TestDetectsMissingKey(t *testing.T) {
	a := Build(dataset(100))
	modified := dataset(100)
	delete(modified, "key:000050")
	b := Build(modified)

	found := false
	for _, k := range Diff(a, b) {
		if k == "key:000050" {
			found = true
		}
	}
	if !found {
		t.Error("a key present in only one replica was not reported")
	}
}

// The efficiency claim, made concrete: O(d log n), not O(n).
func TestDiffCostIsLogarithmic(t *testing.T) {
	const n = 100000
	a := Build(dataset(n))

	modified := dataset(n)
	modified["key:000042"] = "changed"
	b := Build(modified)

	cost := CompareCost(a, b)
	if cost > 100 {
		t.Errorf("used %d comparisons to find 1 difference among %d keys", cost, n)
	}
	if cost >= n {
		t.Error("no better than a full scan")
	}
}

// Equal roots must terminate immediately — that is what makes routine
// anti-entropy checks nearly free.
func TestIdenticalTreesCostOneComparison(t *testing.T) {
	a := Build(dataset(100000))
	b := Build(dataset(100000))

	if cost := CompareCost(a, b); cost != 1 {
		t.Errorf("cost = %d for identical trees, want 1", cost)
	}
}

func TestEmptyTrees(t *testing.T) {
	empty := Build(map[string]string{})
	if empty.RootHash() != "" {
		t.Error("empty tree should have no root hash")
	}
	if len(Diff(empty, Build(map[string]string{}))) != 0 {
		t.Error("two empty trees should not differ")
	}
}

func TestEmptyVersusPopulated(t *testing.T) {
	empty := Build(map[string]string{})
	full := Build(dataset(10))

	if got := Diff(empty, full); len(got) != 10 {
		t.Errorf("Diff = %d keys, want all 10", len(got))
	}
	if got := Diff(full, empty); len(got) != 10 {
		t.Errorf("Diff (reversed) = %d keys, want all 10", len(got))
	}
}

func TestSingleKey(t *testing.T) {
	a := Build(map[string]string{"only": "1"})
	b := Build(map[string]string{"only": "2"})

	if a.RootHash() == b.RootHash() {
		t.Error("single-key trees with different values share a root")
	}
	if got := Diff(a, b); !reflect.DeepEqual(got, []string{"only"}) {
		t.Errorf("Diff = %v, want [only]", got)
	}
}

// Odd leaf counts exercise the promote-rather-than-duplicate branch.
func TestOddLeafCounts(t *testing.T) {
	for _, n := range []int{1, 3, 5, 7, 9, 15, 33, 127, 1001} {
		base := dataset(n)
		a := Build(base)
		if a.Leaves() != n {
			t.Errorf("n=%d: Leaves = %d", n, a.Leaves())
		}

		modified := dataset(n)
		modified[fmt.Sprintf("key:%06d", n-1)] = "changed"
		b := Build(modified)

		if a.RootHash() == b.RootHash() {
			t.Errorf("n=%d: changing the last leaf did not change the root", n)
		}
		if got := Diff(a, b); len(got) != 1 {
			t.Errorf("n=%d: Diff = %v, want 1 key", n, got)
		}
	}
}

// Leaf and internal hashes are domain-separated, so a leaf whose value happens to
// look like a concatenation of hashes cannot forge an internal node.
func TestLeafAndInternalHashesAreDomainSeparated(t *testing.T) {
	l := hashLeaf("a", "b")
	i := hashInternal([]byte("a"), []byte("b"))
	if equalHash(l, i) {
		t.Error("leaf and internal hashing are not domain-separated")
	}
}

// Key/value boundaries must be unambiguous, or {"ab":"c"} and {"a":"bc"} collide.
func TestKeyValueBoundaryIsUnambiguous(t *testing.T) {
	a := Build(map[string]string{"ab": "c"})
	b := Build(map[string]string{"a": "bc"})
	if a.RootHash() == b.RootHash() {
		t.Error("key/value concatenation is ambiguous")
	}
}

func BenchmarkBuild10k(b *testing.B) {
	data := dataset(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(data)
	}
}

func BenchmarkDiffOneChange(b *testing.B) {
	x := Build(dataset(100000))
	modified := dataset(100000)
	modified["key:050000"] = "changed"
	y := Build(modified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Diff(x, y)
	}
}
