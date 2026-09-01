// Package main implements external merge sort: sorting data far larger than RAM.
// See Chapter 13.
//
// This is the algorithm underneath every "sort 100 GB with 2 GB of memory"
// problem — database ORDER BY on a large table, the shuffle phase of MapReduce,
// and `sort(1)` itself.
//
// Two phases:
//
//  1. SPLIT   Read as much as fits in memory, sort it, write a sorted run to
//     disk. Repeat. Cost: O(n log m) where m is the chunk size.
//  2. MERGE   Open all runs at once and repeatedly take the smallest head using
//     a min-heap. Cost: O(n log k) for k runs.
//
// ⭐ The memory bound in phase 2 is what makes this work: merging k runs needs
// only one buffered reader per run plus a k-element heap, regardless of how
// large the runs are. Total memory is O(k), not O(n).
//
// ⚠️ The practical limit is file descriptors and random I/O. Merging 10,000 runs
// in one pass means 10,000 concurrent seeks, which destroys throughput on any
// device. Beyond a few hundred runs you merge in multiple passes.
package main

import (
	"bufio"
	"container/heap"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// Config controls the memory/IO trade-off.
type Config struct {
	// ChunkSize is how many records are held in memory at once during the split
	// phase. This is the real memory bound.
	ChunkSize int
	// TempDir holds the intermediate sorted runs.
	TempDir string
	// MaxMergeWidth caps how many runs are merged in a single pass. Exceeding the
	// file-descriptor limit or thrashing the disk is worse than an extra pass.
	MaxMergeWidth int
}

func DefaultConfig(tempDir string) Config {
	return Config{ChunkSize: 100000, TempDir: tempDir, MaxMergeWidth: 16}
}

// Stats records what the sort actually did, which is the interesting part.
type Stats struct {
	Records     int
	Runs        int
	MergePasses int
	PeakRecords int // the true memory high-water mark, in records
}

// Sort reads integers from in, sorts them, and writes them to out. Only
// ChunkSize records are ever held in memory.
func Sort(in string, out string, cfg Config) (Stats, error) {
	var stats Stats

	runs, n, err := splitIntoSortedRuns(in, cfg)
	if err != nil {
		return stats, err
	}
	defer func() {
		for _, r := range runs {
			os.Remove(r)
		}
	}()

	stats.Records = n
	stats.Runs = len(runs)
	stats.PeakRecords = cfg.ChunkSize

	if len(runs) == 0 {
		return stats, os.WriteFile(out, nil, 0o644)
	}
	if len(runs) == 1 {
		return stats, os.Rename(runs[0], out)
	}

	// Merge in passes so we never open more than MaxMergeWidth files at once.
	current := runs
	for len(current) > 1 {
		stats.MergePasses++
		var next []string

		for i := 0; i < len(current); i += cfg.MaxMergeWidth {
			end := min(i+cfg.MaxMergeWidth, len(current))
			group := current[i:end]

			target := out
			if len(current) > cfg.MaxMergeWidth || end-i < len(current) {
				target = filepath.Join(cfg.TempDir,
					fmt.Sprintf("merge-%d-%d.tmp", stats.MergePasses, i))
			}
			if err := mergeRuns(group, target); err != nil {
				return stats, err
			}
			next = append(next, target)
		}

		for _, r := range current {
			os.Remove(r)
		}
		current = next
	}

	if current[0] != out {
		return stats, os.Rename(current[0], out)
	}
	return stats, nil
}

// splitIntoSortedRuns is phase 1: chunk, sort in memory, spill.
func splitIntoSortedRuns(in string, cfg Config) ([]string, int, error) {
	f, err := os.Open(in)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return nil, 0, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var runs []string
	buf := make([]int, 0, cfg.ChunkSize)
	total := 0

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		sort.Ints(buf)
		path := filepath.Join(cfg.TempDir, fmt.Sprintf("run-%04d.tmp", len(runs)))
		if err := writeInts(path, buf); err != nil {
			return err
		}
		runs = append(runs, path)
		buf = buf[:0]
		return nil
	}

	for scanner.Scan() {
		v, err := strconv.Atoi(scanner.Text())
		if err != nil {
			return nil, 0, fmt.Errorf("parsing %q: %w", scanner.Text(), err)
		}
		buf = append(buf, v)
		total++
		if len(buf) >= cfg.ChunkSize {
			if err := flush(); err != nil {
				return nil, 0, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if err := flush(); err != nil {
		return nil, 0, err
	}
	return runs, total, nil
}

// heapItem is one run's current head value.
type heapItem struct {
	value  int
	source int
}

type minHeap []heapItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].value < h[j].value }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(heapItem)) }
func (h *minHeap) Pop() any          { old := *h; n := len(old); it := old[n-1]; *h = old[:n-1]; return it }

// mergeRuns is phase 2: a k-way merge using a min-heap.
//
// ⭐ Memory here is O(k) — one buffered reader and one heap entry per run — no
// matter how large the runs are. That is the whole trick.
func mergeRuns(runs []string, out string) error {
	files := make([]*os.File, 0, len(runs))
	readers := make([]*bufio.Scanner, 0, len(runs))
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()

	for _, path := range runs {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		files = append(files, f)
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		readers = append(readers, s)
	}

	h := &minHeap{}
	heap.Init(h)
	for i, r := range readers {
		if v, ok := nextInt(r); ok {
			heap.Push(h, heapItem{value: v, source: i})
		}
	}

	of, err := os.Create(out)
	if err != nil {
		return err
	}
	defer of.Close()
	w := bufio.NewWriter(of)
	defer w.Flush()

	for h.Len() > 0 {
		it := heap.Pop(h).(heapItem)
		if _, err := w.WriteString(strconv.Itoa(it.value)); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
		// Pull the next value from the run we just consumed from.
		if v, ok := nextInt(readers[it.source]); ok {
			heap.Push(h, heapItem{value: v, source: it.source})
		}
	}
	return nil
}

func nextInt(s *bufio.Scanner) (int, bool) {
	for s.Scan() {
		line := s.Text()
		if line == "" {
			continue
		}
		v, err := strconv.Atoi(line)
		if err != nil {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func writeInts(path string, values []int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, v := range values {
		w.WriteString(strconv.Itoa(v))
		w.WriteByte('\n')
	}
	return w.Flush()
}

func readInts(path string) ([]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []int
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		if s.Text() == "" {
			continue
		}
		v, err := strconv.Atoi(s.Text())
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, s.Err()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	dir, _ := os.MkdirTemp("", "extsort")
	defer os.RemoveAll(dir)

	const n = 1000000
	input := filepath.Join(dir, "input.txt")

	rng := rand.New(rand.NewSource(42))
	values := make([]int, n)
	for i := range values {
		values[i] = rng.Intn(10000000)
	}
	writeInts(input, values)

	info, _ := os.Stat(input)
	fmt.Printf("input: %d records, %.1f MB\n", n, float64(info.Size())/(1<<20))

	// Deliberately tiny: hold 1% of the dataset at a time.
	cfg := Config{ChunkSize: n / 100, TempDir: dir, MaxMergeWidth: 8}
	output := filepath.Join(dir, "sorted.txt")

	stats, err := Sort(input, output, cfg)
	if err != nil {
		panic(err)
	}

	fmt.Printf("\nchunk size: %d records (%.0f%% of the dataset)\n",
		cfg.ChunkSize, 100*float64(cfg.ChunkSize)/float64(n))
	fmt.Printf("sorted runs produced: %d\n", stats.Runs)
	fmt.Printf("merge passes: %d (max %d runs open at once)\n", stats.MergePasses, cfg.MaxMergeWidth)
	fmt.Printf("peak memory: %d records, never the full %d\n", stats.PeakRecords, stats.Records)

	sorted, _ := readInts(output)
	fmt.Printf("\noutput: %d records, sorted=%v\n", len(sorted), sort.IntsAreSorted(sorted))
	fmt.Printf("first 5: %v\n", sorted[:5])
	fmt.Printf("last 5:  %v\n", sorted[len(sorted)-5:])
}
