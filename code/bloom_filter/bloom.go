// Package main implements a Bloom filter with the standard sizing formulas.
// See Chapter 23.
//
// Guarantee: no false negatives. If Contains returns false, the item was
// definitely never added. If it returns true, the item is probably present.
// That asymmetry is what makes it useful as a cheap pre-filter in front of an
// expensive exact lookup (SSTable reads, crawler URL dedup, click dedup).
package main

import (
	"fmt"
	"hash/fnv"
	"math"
)

type BloomFilter struct {
	bits  []uint64
	m     uint64 // bit count
	k     uint64 // hash count
	added uint64
}

// NewBloomFilter sizes the filter from the expected item count and target
// false-positive rate:
//
//	m = -n * ln(p) / (ln 2)^2
//	k = (m/n) * ln 2
func NewBloomFilter(expectedItems int, falsePositiveRate float64) *BloomFilter {
	if expectedItems <= 0 {
		expectedItems = 1
	}
	if falsePositiveRate <= 0 || falsePositiveRate >= 1 {
		panic("bloom: falsePositiveRate must be in (0,1)")
	}
	n := float64(expectedItems)
	m := math.Ceil(-n * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2))
	k := math.Round(m / n * math.Ln2)
	if k < 1 {
		k = 1
	}
	return &BloomFilter{
		bits: make([]uint64, (uint64(m)+63)/64),
		m:    uint64(m),
		k:    uint64(k),
	}
}

// hashes derives k indices from two hashes (Kirsch-Mitzenmacher), which is
// statistically as good as k independent hash functions and much cheaper.
func (b *BloomFilter) hashes(data []byte) (uint64, uint64) {
	h := fnv.New64a()
	h.Write(data)
	h1 := h.Sum64()
	h.Write([]byte{0xff})
	h2 := h.Sum64() | 1 // odd, so it strides the whole space
	return h1, h2
}

func (b *BloomFilter) Add(item string) {
	h1, h2 := b.hashes([]byte(item))
	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.m
		b.bits[idx/64] |= 1 << (idx % 64)
	}
	b.added++
}

func (b *BloomFilter) Contains(item string) bool {
	h1, h2 := b.hashes([]byte(item))
	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.m
		if b.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false // definitely absent
		}
	}
	return true // probably present
}

func (b *BloomFilter) Bits() uint64      { return b.m }
func (b *BloomFilter) HashCount() uint64 { return b.k }
func (b *BloomFilter) SizeBytes() uint64 { return uint64(len(b.bits)) * 8 }

// EstimatedFalsePositiveRate uses the actual insert count, so it reflects
// over-filling: exceed the expected item count and this climbs sharply.
func (b *BloomFilter) EstimatedFalsePositiveRate() float64 {
	exponent := -float64(b.k) * float64(b.added) / float64(b.m)
	return math.Pow(1-math.Exp(exponent), float64(b.k))
}

func main() {
	const n = 10_000_000
	const p = 0.01

	bf := NewBloomFilter(n, p)
	fmt.Printf("sizing for %d items at %.0f%% false positives:\n", n, p*100)
	fmt.Printf("  bits: %d  hashes: %d  memory: %.1f MB\n",
		bf.Bits(), bf.HashCount(), float64(bf.SizeBytes())/(1<<20))
	fmt.Printf("  an exact hash set of 20-byte keys would need ~%.0f MB\n",
		float64(n)*float64(20+48)/(1<<20))

	for i := 0; i < 100000; i++ {
		bf.Add(fmt.Sprintf("url:%d", i))
	}

	// No false negatives, ever.
	missing := 0
	for i := 0; i < 100000; i++ {
		if !bf.Contains(fmt.Sprintf("url:%d", i)) {
			missing++
		}
	}
	fmt.Printf("\nfalse negatives among 100000 inserted items: %d (must be 0)\n", missing)

	fp := 0
	for i := 0; i < 100000; i++ {
		if bf.Contains(fmt.Sprintf("absent:%d", i)) {
			fp++
		}
	}
	fmt.Printf("false positives among 100000 absent items: %d (%.3f%%)\n",
		fp, 100*float64(fp)/100000)
	fmt.Printf("predicted rate at this fill level: %.5f%%\n",
		100*bf.EstimatedFalsePositiveRate())
}
