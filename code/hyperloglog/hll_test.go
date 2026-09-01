package main

import (
	"fmt"
	"math"
	"testing"
)

func relErr(est uint64, truth int) float64 {
	return math.Abs(float64(est)-float64(truth)) / float64(truth)
}

func TestAccuracyAcrossMagnitudes(t *testing.T) {
	for _, n := range []int{1000, 10000, 100000, 1000000} {
		h := NewHyperLogLog(14)
		for i := 0; i < n; i++ {
			h.Add(fmt.Sprintf("item:%d", i))
		}
		// Allow 3 standard errors.
		tolerance := 3 * h.StdError()
		if e := relErr(h.Count(), n); e > tolerance {
			t.Errorf("n=%d: error %.3f%% exceeds %.3f%%", n, e*100, tolerance*100)
		}
	}
}

// Linear counting must take over at low cardinality, where the raw estimator is bad.
func TestSmallCardinalityIsExactish(t *testing.T) {
	h := NewHyperLogLog(14)
	for i := 0; i < 100; i++ {
		h.Add(fmt.Sprintf("item:%d", i))
	}
	if e := relErr(h.Count(), 100); e > 0.05 {
		t.Errorf("error %.2f%% at n=100, want under 5%% via linear counting", e*100)
	}
}

func TestEmpty(t *testing.T) {
	if got := NewHyperLogLog(14).Count(); got != 0 {
		t.Errorf("Count = %d, want 0", got)
	}
}

func TestDuplicatesDoNotInflate(t *testing.T) {
	h := NewHyperLogLog(14)
	for i := 0; i < 1000; i++ {
		for r := 0; r < 100; r++ {
			h.Add(fmt.Sprintf("item:%d", i))
		}
	}
	if e := relErr(h.Count(), 1000); e > 0.05 {
		t.Errorf("100x duplicates shifted the estimate by %.2f%%", e*100)
	}
}

// Mergeability is why HLL works across shards: union without re-scanning.
func TestMergeUnionsCorrectly(t *testing.T) {
	a, b := NewHyperLogLog(14), NewHyperLogLog(14)
	for i := 0; i < 500000; i++ {
		a.Add(fmt.Sprintf("u:%d", i))
	}
	for i := 250000; i < 750000; i++ {
		b.Add(fmt.Sprintf("u:%d", i))
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	if e := relErr(a.Count(), 750000); e > 3*a.StdError() {
		t.Errorf("merged error %.3f%%", e*100)
	}
}

func TestMergeDisjoint(t *testing.T) {
	a, b := NewHyperLogLog(14), NewHyperLogLog(14)
	for i := 0; i < 100000; i++ {
		a.Add(fmt.Sprintf("a:%d", i))
		b.Add(fmt.Sprintf("b:%d", i))
	}
	a.Merge(b)
	if e := relErr(a.Count(), 200000); e > 3*a.StdError() {
		t.Errorf("disjoint merge error %.3f%%", e*100)
	}
}

func TestMergeRejectsPrecisionMismatch(t *testing.T) {
	if err := NewHyperLogLog(14).Merge(NewHyperLogLog(12)); err == nil {
		t.Error("merging different precisions should fail")
	}
}

// Error should fall as 1.04/sqrt(m): doubling precision bits halves it.
func TestHigherPrecisionLowersError(t *testing.T) {
	low := NewHyperLogLog(10)
	high := NewHyperLogLog(16)

	if high.StdError() >= low.StdError() {
		t.Error("higher precision must reduce standard error")
	}
	ratio := low.StdError() / high.StdError()
	if ratio < 7 || ratio > 9 {
		t.Errorf("error ratio %.2f, want ~8 (sqrt of 64x registers)", ratio)
	}
	if high.SizeBytes() != 65536 {
		t.Errorf("size = %d, want 65536", high.SizeBytes())
	}
}

func TestPrecisionBounds(t *testing.T) {
	for _, p := range []uint8{3, 19} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("precision %d should panic", p)
				}
			}()
			NewHyperLogLog(p)
		}()
	}
}

func BenchmarkAdd(b *testing.B) {
	h := NewHyperLogLog(14)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Add(fmt.Sprintf("item:%d", i))
	}
}
