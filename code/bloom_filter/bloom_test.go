package main

import (
	"fmt"
	"math"
	"testing"
)

// The defining property: a Bloom filter may lie about presence, never about absence.
func TestNoFalseNegatives(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)
	for i := 0; i < 10000; i++ {
		bf.Add(fmt.Sprintf("item:%d", i))
	}
	for i := 0; i < 10000; i++ {
		if !bf.Contains(fmt.Sprintf("item:%d", i)) {
			t.Fatalf("false negative for item:%d — this must never happen", i)
		}
	}
}

func TestFalsePositiveRateNearTarget(t *testing.T) {
	const n = 100000
	const target = 0.01

	bf := NewBloomFilter(n, target)
	for i := 0; i < n; i++ {
		bf.Add(fmt.Sprintf("present:%d", i))
	}

	fp := 0
	const trials = 100000
	for i := 0; i < trials; i++ {
		if bf.Contains(fmt.Sprintf("absent:%d", i)) {
			fp++
		}
	}

	observed := float64(fp) / trials
	if observed > target*2 {
		t.Errorf("false positive rate %.4f, more than double the %.4f target", observed, target)
	}
}

// m = -n*ln(p)/(ln2)^2 gives ~9.6 bits per item at 1%, and k = 7.
func TestSizingFormula(t *testing.T) {
	bf := NewBloomFilter(1000000, 0.01)

	bitsPerItem := float64(bf.Bits()) / 1000000
	if bitsPerItem < 9 || bitsPerItem > 10.5 {
		t.Errorf("bits per item = %.2f, want ~9.6", bitsPerItem)
	}
	if bf.HashCount() != 7 {
		t.Errorf("hash count = %d, want 7", bf.HashCount())
	}
}

// Tightening the target rate must cost memory; each 10x costs ~4.8 bits/item.
func TestTighterRateCostsMemory(t *testing.T) {
	loose := NewBloomFilter(100000, 0.1)
	tight := NewBloomFilter(100000, 0.001)

	if tight.Bits() <= loose.Bits() {
		t.Error("a 0.1% filter should need more bits than a 10% one")
	}
	ratio := float64(tight.Bits()) / float64(loose.Bits())
	if ratio < 2.5 || ratio > 3.5 {
		t.Errorf("bit ratio = %.2f, want ~3 (two decades at ~4.8 bits each)", ratio)
	}
}

// Over-filling degrades the filter rather than failing loudly — the trap.
func TestOverfillingDegradesRate(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	for i := 0; i < 10000; i++ {
		bf.Add(fmt.Sprintf("item:%d", i))
	}

	predicted := bf.EstimatedFalsePositiveRate()
	if predicted < 0.5 {
		t.Errorf("estimated FP rate at 10x overfill = %.3f, expected it to be severe", predicted)
	}
}

func TestEstimateTracksObserved(t *testing.T) {
	const n = 50000
	bf := NewBloomFilter(n, 0.01)
	for i := 0; i < n; i++ {
		bf.Add(fmt.Sprintf("k:%d", i))
	}

	fp := 0
	const trials = 50000
	for i := 0; i < trials; i++ {
		if bf.Contains(fmt.Sprintf("nope:%d", i)) {
			fp++
		}
	}

	observed := float64(fp) / trials
	predicted := bf.EstimatedFalsePositiveRate()
	if math.Abs(observed-predicted) > 0.01 {
		t.Errorf("observed %.4f vs predicted %.4f — formula and reality disagree", observed, predicted)
	}
}

func TestEmptyFilterContainsNothing(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)
	for i := 0; i < 1000; i++ {
		if bf.Contains(fmt.Sprintf("x:%d", i)) {
			t.Fatal("empty filter reported a member")
		}
	}
}

func BenchmarkAdd(b *testing.B) {
	bf := NewBloomFilter(b.N+1, 0.01)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(fmt.Sprintf("item:%d", i))
	}
}

func BenchmarkContains(b *testing.B) {
	bf := NewBloomFilter(100000, 0.01)
	for i := 0; i < 100000; i++ {
		bf.Add(fmt.Sprintf("item:%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Contains("item:500")
	}
}
