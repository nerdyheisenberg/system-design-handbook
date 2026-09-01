package main

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func TestPutGet(t *testing.T) {
	tree := New()
	for _, k := range []string{"d", "a", "f", "b", "h", "c", "g", "e"} {
		tree.Put(k, "v-"+k)
	}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		v, ok := tree.Get(k)
		if !ok || v != "v-"+k {
			t.Errorf("Get(%q) = %q,%v", k, v, ok)
		}
	}
	if _, ok := tree.Get("z"); ok {
		t.Error("absent key reported as found")
	}
}

func TestEmptyTree(t *testing.T) {
	tree := New()
	if _, ok := tree.Get("anything"); ok {
		t.Error("empty tree returned a value")
	}
	if tree.Len() != 0 || tree.Height() != 1 {
		t.Errorf("Len = %d, Height = %d, want 0 and 1", tree.Len(), tree.Height())
	}
}

func TestOverwriteDoesNotGrow(t *testing.T) {
	tree := New()
	tree.Put("k", "v1")
	tree.Put("k", "v2")

	if tree.Len() != 1 {
		t.Errorf("Len = %d, want 1", tree.Len())
	}
	if v, _ := tree.Get("k"); v != "v2" {
		t.Errorf("value = %q, want \"v2\"", v)
	}
}

// Splits must keep every key reachable — the classic B-tree bug is losing one.
func TestSplitsPreserveAllKeys(t *testing.T) {
	tree := New()
	const n = 5000
	for i := 0; i < n; i++ {
		tree.Put(fmt.Sprintf("key:%05d", i), fmt.Sprintf("v%d", i))
	}
	if tree.Len() != n {
		t.Fatalf("Len = %d, want %d", tree.Len(), n)
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("v%d", i)
		if v, ok := tree.Get(fmt.Sprintf("key:%05d", i)); !ok || v != want {
			t.Fatalf("key:%05d = %q,%v want %q — lost in a split", i, v, ok, want)
		}
	}
}

func TestRandomInsertionOrder(t *testing.T) {
	tree := New()
	rng := rand.New(rand.NewSource(42))
	keys := make([]string, 2000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key:%05d", i)
	}
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })

	for _, k := range keys {
		tree.Put(k, "v")
	}
	got := tree.Keys()
	if !sort.StringsAreSorted(got) {
		t.Fatal("keys are not sorted after random insertion")
	}
	if len(got) != len(keys) {
		t.Errorf("got %d keys, want %d", len(got), len(keys))
	}
}

// Sequential insertion is the worst case for many trees; a B+tree must stay balanced.
func TestSequentialInsertionStaysBalanced(t *testing.T) {
	tree := New()
	const n = 10000
	for i := 0; i < n; i++ {
		tree.Put(fmt.Sprintf("key:%06d", i), "v")
	}
	// Height should be O(log_order n), not O(n).
	maxHeight := int(math.Log(float64(n))/math.Log(float64(order)/2)) + 2
	if tree.Height() > maxHeight {
		t.Errorf("height = %d for %d keys, want at most %d", tree.Height(), n, maxHeight)
	}
}

// Values live only in leaves and leaves are chained; a broken chain silently
// breaks every range scan.
func TestLeafChainIsIntact(t *testing.T) {
	tree := New()
	for i := 0; i < 3000; i++ {
		tree.Put(fmt.Sprintf("key:%05d", i), "v")
	}
	if !tree.LeafChainIntact() {
		t.Fatal("leaf chain is broken or unsorted")
	}
}

func TestRangeScan(t *testing.T) {
	tree := New()
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		tree.Put(k, k)
	}

	want := []string{"c=c", "d=d", "e=e"}
	if got := tree.Range("c", "e"); !reflect.DeepEqual(got, want) {
		t.Errorf("Range(c,e) = %v, want %v", got, want)
	}
}

func TestRangeScanSpansMultipleLeaves(t *testing.T) {
	tree := New()
	for i := 0; i < 1000; i++ {
		tree.Put(fmt.Sprintf("key:%04d", i), "v")
	}

	got := tree.Range("key:0100", "key:0199")
	if len(got) != 100 {
		t.Errorf("range returned %d entries, want 100 — the leaf chain must be followed", len(got))
	}
}

func TestRangeScanBoundaries(t *testing.T) {
	tree := New()
	for i := 0; i < 100; i++ {
		tree.Put(fmt.Sprintf("k%03d", i), "v")
	}

	if got := tree.Range("k000", "k099"); len(got) != 100 {
		t.Errorf("full range returned %d, want 100", len(got))
	}
	if got := tree.Range("k050", "k050"); len(got) != 1 {
		t.Errorf("single-key range returned %d, want 1", len(got))
	}
	if got := tree.Range("z000", "z999"); len(got) != 0 {
		t.Errorf("empty range returned %d, want 0", len(got))
	}
}

func TestRangeStartBeforeFirstKey(t *testing.T) {
	tree := New()
	for _, k := range []string{"m", "n", "o"} {
		tree.Put(k, k)
	}
	if got := tree.Range("a", "z"); len(got) != 3 {
		t.Errorf("got %d, want 3", len(got))
	}
}

func TestHeightGrowsLogarithmically(t *testing.T) {
	prev := 0
	for _, n := range []int{10, 100, 1000, 10000, 100000} {
		tree := New()
		for i := 0; i < n; i++ {
			tree.Put(fmt.Sprintf("key:%08d", i), "v")
		}
		h := tree.Height()
		if h < prev {
			t.Errorf("height decreased from %d to %d as n grew", prev, h)
		}
		// 10x the keys must not cost anywhere near 10x the height.
		if prev > 0 && h > prev+3 {
			t.Errorf("height jumped from %d to %d for 10x keys", prev, h)
		}
		prev = h
	}
}

func TestDuplicateKeysAcrossSplits(t *testing.T) {
	tree := New()
	for i := 0; i < 500; i++ {
		tree.Put(fmt.Sprintf("key:%03d", i), "first")
	}
	for i := 0; i < 500; i++ {
		tree.Put(fmt.Sprintf("key:%03d", i), "second")
	}

	if tree.Len() != 500 {
		t.Errorf("Len = %d, want 500", tree.Len())
	}
	for i := 0; i < 500; i++ {
		if v, _ := tree.Get(fmt.Sprintf("key:%03d", i)); v != "second" {
			t.Fatalf("key:%03d = %q, want \"second\"", i, v)
		}
	}
}

func BenchmarkPut(b *testing.B) {
	tree := New()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Put(fmt.Sprintf("key:%08d", i), "v")
	}
}

func BenchmarkGet(b *testing.B) {
	tree := New()
	for i := 0; i < 100000; i++ {
		tree.Put(fmt.Sprintf("key:%08d", i), "v")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Get(fmt.Sprintf("key:%08d", i%100000))
	}
}

func BenchmarkRangeScan100(b *testing.B) {
	tree := New()
	for i := 0; i < 100000; i++ {
		tree.Put(fmt.Sprintf("key:%08d", i), "v")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Range("key:00050000", "key:00050099")
	}
}
