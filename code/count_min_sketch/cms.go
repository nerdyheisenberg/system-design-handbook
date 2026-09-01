// Package main implements a Count-Min Sketch for frequency estimation in
// sub-linear space. See Chapter 23.
//
// Guarantee: the estimate never under-counts. Collisions can only inflate a
// counter, so Estimate returns true count <= estimate <= true count + error.
// Taking the minimum across d rows makes over-counting unlikely.
//
// Used for hot-key detection, heavy hitters, and per-key rate limiting where
// tracking every key exactly would not fit in memory.
package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
)

type CountMinSketch struct {
	counts [][]uint64
	w      uint64 // columns
	d      uint64 // rows, one per hash function
	total  uint64
}

// NewCountMinSketch sizes from the error bound epsilon and failure probability delta:
//
//	w = ceil(e / epsilon)      d = ceil(ln(1/delta))
//
// The estimate is then within epsilon * total_count with probability 1 - delta.
func NewCountMinSketch(epsilon, delta float64) *CountMinSketch {
	if epsilon <= 0 || epsilon >= 1 {
		panic("cms: epsilon must be in (0,1)")
	}
	if delta <= 0 || delta >= 1 {
		panic("cms: delta must be in (0,1)")
	}
	w := uint64(math.Ceil(math.E / epsilon))
	d := uint64(math.Ceil(math.Log(1 / delta)))

	counts := make([][]uint64, d)
	for i := range counts {
		counts[i] = make([]uint64, w)
	}
	return &CountMinSketch{counts: counts, w: w, d: d}
}

func (c *CountMinSketch) hashes(item string) (uint64, uint64) {
	h := fnv.New64a()
	h.Write([]byte(item))
	h1 := h.Sum64()
	h.Write([]byte{0xff})
	h2 := h.Sum64() | 1
	return h1, h2
}

func (c *CountMinSketch) Add(item string, count uint64) {
	h1, h2 := c.hashes(item)
	for i := uint64(0); i < c.d; i++ {
		c.counts[i][(h1+i*h2)%c.w] += count
	}
	c.total += count
}

// Estimate returns the minimum across rows, which is the tightest upper bound
// available: every row's counter includes this item plus any collisions.
func (c *CountMinSketch) Estimate(item string) uint64 {
	h1, h2 := c.hashes(item)
	min := uint64(math.MaxUint64)
	for i := uint64(0); i < c.d; i++ {
		if v := c.counts[i][(h1+i*h2)%c.w]; v < min {
			min = v
		}
	}
	return min
}

func (c *CountMinSketch) Total() uint64     { return c.total }
func (c *CountMinSketch) Width() uint64     { return c.w }
func (c *CountMinSketch) Depth() uint64     { return c.d }
func (c *CountMinSketch) SizeBytes() uint64 { return c.w * c.d * 8 }

// ErrorBound is the absolute over-count the sketch guarantees (with probability
// 1-delta) at the current total.
func (c *CountMinSketch) ErrorBound() float64 {
	return math.E / float64(c.w) * float64(c.total)
}

// HeavyHitters returns candidates from the given key set exceeding a fraction of
// the total. The sketch cannot enumerate keys, so callers supply the candidates —
// typically from a sampled stream.
func (c *CountMinSketch) HeavyHitters(candidates []string, fraction float64) []string {
	threshold := uint64(fraction * float64(c.total))
	var out []string
	for _, k := range candidates {
		if c.Estimate(k) >= threshold {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return c.Estimate(out[i]) > c.Estimate(out[j]) })
	return out
}

func main() {
	cms := NewCountMinSketch(0.001, 0.01)
	fmt.Printf("sketch: %d x %d counters = %.1f KB\n",
		cms.Depth(), cms.Width(), float64(cms.SizeBytes())/1024)

	// A Zipf-ish stream: a few keys dominate, a long tail does not.
	truth := map[string]uint64{}
	add := func(k string, n uint64) {
		cms.Add(k, n)
		truth[k] += n
	}
	add("hot:1", 500000)
	add("hot:2", 250000)
	add("hot:3", 100000)
	for i := 0; i < 200000; i++ {
		add(fmt.Sprintf("tail:%d", i), 1)
	}

	fmt.Printf("\ntotal events: %d\n", cms.Total())
	fmt.Printf("guaranteed error bound: +/- %.0f\n\n", cms.ErrorBound())
	fmt.Printf("%-10s %10s %10s %8s\n", "key", "true", "estimate", "error")
	for _, k := range []string{"hot:1", "hot:2", "hot:3", "tail:5", "tail:99999"} {
		est := cms.Estimate(k)
		fmt.Printf("%-10s %10d %10d %+8d\n", k, truth[k], est, int64(est)-int64(truth[k]))
	}

	candidates := []string{"hot:1", "hot:2", "hot:3", "tail:5"}
	fmt.Println("\nkeys above 5% of traffic:", cms.HeavyHitters(candidates, 0.05))
}
