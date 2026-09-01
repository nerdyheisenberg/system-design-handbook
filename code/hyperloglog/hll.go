// Package main implements HyperLogLog for cardinality estimation. See Chapter 23.
//
// The idea: hash each item and look at the position of the leftmost 1 bit. Seeing
// a hash starting with k zeros suggests roughly 2^k distinct items, because that
// pattern has probability 2^-k. One such observation is far too noisy, so the hash
// is split into 2^p buckets, each keeping its own maximum, and the harmonic mean
// across buckets cancels the variance.
//
// Result: count billions of distinct items in ~12 KB at ~2% error. An exact set
// would need gigabytes.
package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
)

type HyperLogLog struct {
	registers []uint8
	p         uint8  // bucket index bits
	m         uint32 // 1 << p buckets
	alpha     float64
}

// NewHyperLogLog builds a counter with 2^precision registers.
// precision 14 -> 16384 registers -> 16 KB -> ~0.81% standard error.
func NewHyperLogLog(precision uint8) *HyperLogLog {
	if precision < 4 || precision > 18 {
		panic("hll: precision must be in [4,18]")
	}
	m := uint32(1) << precision
	return &HyperLogLog{
		registers: make([]uint8, m),
		p:         precision,
		m:         m,
		alpha:     alphaFor(m),
	}
}

// alphaFor is the bias-correction constant for the harmonic mean.
func alphaFor(m uint32) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/float64(m))
	}
}

// mix is the murmur3 64-bit finalizer. FNV-1a avalanches poorly in its high
// bits, and those are exactly the bits that select the register, so without
// this the buckets cluster badly and the estimate collapses.
func mix(x uint64) uint64 {
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

func (h *HyperLogLog) Add(item string) {
	f := fnv.New64a()
	f.Write([]byte(item))
	x := mix(f.Sum64())

	idx := uint32(x >> (64 - h.p))        // top p bits pick the register
	rest := (x << h.p) | (1 << (h.p - 1)) // remaining bits, padded so it is never 0
	rank := uint8(bits.LeadingZeros64(rest)) + 1

	if rank > h.registers[idx] {
		h.registers[idx] = rank
	}
}

// Count returns the estimated cardinality, with the small- and large-range
// corrections from the original paper.
func (h *HyperLogLog) Count() uint64 {
	sum := 0.0
	zeros := 0
	for _, r := range h.registers {
		sum += 1 / math.Pow(2, float64(r))
		if r == 0 {
			zeros++
		}
	}

	estimate := h.alpha * float64(h.m) * float64(h.m) / sum

	// Small range: with empty registers, linear counting is far more accurate.
	if estimate <= 2.5*float64(h.m) && zeros > 0 {
		return uint64(float64(h.m) * math.Log(float64(h.m)/float64(zeros)))
	}
	// Large range: correct for 32-bit hash collisions.
	if estimate > (1.0/30.0)*math.Pow(2, 32) {
		return uint64(-math.Pow(2, 32) * math.Log(1-estimate/math.Pow(2, 32)))
	}
	return uint64(estimate)
}

// Merge unions another counter of the same precision. This is why HLL is used in
// distributed systems: per-shard counters combine losslessly with a max per register.
func (h *HyperLogLog) Merge(other *HyperLogLog) error {
	if h.p != other.p {
		return fmt.Errorf("hll: precision mismatch %d vs %d", h.p, other.p)
	}
	for i, r := range other.registers {
		if r > h.registers[i] {
			h.registers[i] = r
		}
	}
	return nil
}

func (h *HyperLogLog) SizeBytes() int { return len(h.registers) }

// StdError is the relative standard error, 1.04/sqrt(m).
func (h *HyperLogLog) StdError() float64 { return 1.04 / math.Sqrt(float64(h.m)) }

func main() {
	hll := NewHyperLogLog(14)
	fmt.Printf("precision 14: %d registers, %d bytes, %.2f%% standard error\n\n",
		hll.m, hll.SizeBytes(), hll.StdError()*100)

	fmt.Printf("%12s %12s %8s\n", "true", "estimated", "error")
	for _, n := range []int{100, 1000, 10000, 100000, 1000000, 10000000} {
		h := NewHyperLogLog(14)
		for i := 0; i < n; i++ {
			h.Add(fmt.Sprintf("user:%d", i))
		}
		est := h.Count()
		errPct := 100 * (float64(est) - float64(n)) / float64(n)
		fmt.Printf("%12d %12d %+7.2f%%\n", n, est, errPct)
	}

	// Merging is what makes it usable across shards.
	a, b := NewHyperLogLog(14), NewHyperLogLog(14)
	for i := 0; i < 500000; i++ {
		a.Add(fmt.Sprintf("u:%d", i))
	}
	for i := 250000; i < 750000; i++ { // 250k overlap
		b.Add(fmt.Sprintf("u:%d", i))
	}
	a.Merge(b)
	fmt.Printf("\nmerged two shards with overlap: %d distinct (true 750000)\n", a.Count())
	fmt.Printf("exact counting would need ~%.0f MB; this used %d KB\n",
		float64(750000*24)/(1<<20), hll.SizeBytes()/1024)
}
