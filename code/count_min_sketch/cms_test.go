package main

import (
	"fmt"
	"testing"
)

// The defining property: collisions can only add, never subtract.
func TestNeverUnderCounts(t *testing.T) {
	cms := NewCountMinSketch(0.01, 0.01)
	truth := map[string]uint64{}

	for i := 0; i < 5000; i++ {
		k := fmt.Sprintf("key:%d", i%500)
		cms.Add(k, 1)
		truth[k]++
	}
	for k, want := range truth {
		if got := cms.Estimate(k); got < want {
			t.Fatalf("under-counted %s: estimate %d < true %d", k, got, want)
		}
	}
}

func TestUnseenKeysEstimateZeroOrSmall(t *testing.T) {
	cms := NewCountMinSketch(0.0001, 0.01)
	for i := 0; i < 1000; i++ {
		cms.Add(fmt.Sprintf("present:%d", i), 1)
	}

	overs := 0
	for i := 0; i < 1000; i++ {
		if cms.Estimate(fmt.Sprintf("absent:%d", i)) > 0 {
			overs++
		}
	}
	if overs > 50 {
		t.Errorf("%d/1000 absent keys estimated non-zero — sketch too small", overs)
	}
}

// Heavy hitters are what the structure is for: the tail's collision noise is
// negligible relative to a hot key's count.
func TestHeavyHittersAreAccurate(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	cms.Add("hot", 1000000)
	for i := 0; i < 100000; i++ {
		cms.Add(fmt.Sprintf("cold:%d", i), 1)
	}

	est := cms.Estimate("hot")
	errPct := float64(est-1000000) / 1000000 * 100
	if errPct > 1 {
		t.Errorf("hot key estimate off by %.2f%%, want under 1%%", errPct)
	}
}

func TestRespectsErrorBound(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.001)
	truth := map[string]uint64{}
	for i := 0; i < 100000; i++ {
		k := fmt.Sprintf("k:%d", i%10000)
		cms.Add(k, 1)
		truth[k]++
	}

	bound := cms.ErrorBound()
	violations := 0
	for k, want := range truth {
		if overcount := float64(cms.Estimate(k) - want); overcount > bound {
			violations++
		}
	}
	// delta=0.001 allows ~0.1% of keys to exceed the bound.
	if violations > len(truth)/100 {
		t.Errorf("%d/%d keys exceeded the error bound %.1f", violations, len(truth), bound)
	}
}

func TestSizingFormula(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	if cms.Width() != 2719 { // ceil(e/0.001)
		t.Errorf("width = %d, want 2719", cms.Width())
	}
	if cms.Depth() != 5 { // ceil(ln(1/0.01))
		t.Errorf("depth = %d, want 5", cms.Depth())
	}
}

// Tightening epsilon widens the table; tightening delta deepens it.
func TestTighterEpsilonWidensTable(t *testing.T) {
	loose := NewCountMinSketch(0.01, 0.01)
	tight := NewCountMinSketch(0.0001, 0.01)

	if tight.Width() <= loose.Width() {
		t.Error("smaller epsilon should widen the table")
	}
	if tight.Depth() != loose.Depth() {
		t.Error("epsilon should not affect depth")
	}
}

func TestAddWithWeight(t *testing.T) {
	cms := NewCountMinSketch(0.001, 0.01)
	cms.Add("k", 500)
	if got := cms.Estimate("k"); got != 500 {
		t.Errorf("Estimate = %d, want 500", got)
	}
	if cms.Total() != 500 {
		t.Errorf("Total = %d, want 500", cms.Total())
	}
}

func TestHeavyHittersOrdering(t *testing.T) {
	cms := NewCountMinSketch(0.0001, 0.01)
	cms.Add("a", 500)
	cms.Add("b", 300)
	cms.Add("c", 150)
	cms.Add("d", 50)

	got := cms.HeavyHitters([]string{"a", "b", "c", "d"}, 0.1)
	if len(got) != 3 {
		t.Fatalf("got %v, want 3 keys above 10%%", got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want descending by frequency", got)
	}
}

func BenchmarkAdd(b *testing.B) {
	cms := NewCountMinSketch(0.001, 0.01)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cms.Add("key", 1)
	}
}
