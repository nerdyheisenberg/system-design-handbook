package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func writeTestInput(t *testing.T, dir string, values []int) string {
	t.Helper()
	path := filepath.Join(dir, "input.txt")
	if err := writeInts(path, values); err != nil {
		t.Fatal(err)
	}
	return path
}

func randomInts(n int, seed int64) []int {
	rng := rand.New(rand.NewSource(seed))
	out := make([]int, n)
	for i := range out {
		out[i] = rng.Intn(1000000)
	}
	return out
}

func TestSortsCorrectly(t *testing.T) {
	dir := t.TempDir()
	values := randomInts(10000, 1)
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 500, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}

	got, err := readInts(out)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]int(nil), values...)
	sort.Ints(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output is not the sorted input (got %d records, want %d)", len(got), len(want))
	}
}

// The defining property: the chunk size, not the dataset size, bounds memory.
func TestChunkSizeBoundsRuns(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, randomInts(10000, 2))
	out := filepath.Join(dir, "out.txt")

	stats, err := Sort(in, out, Config{ChunkSize: 1000, TempDir: dir, MaxMergeWidth: 100})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Runs != 10 {
		t.Errorf("Runs = %d, want 10 (10000 records / 1000 per chunk)", stats.Runs)
	}
	if stats.PeakRecords != 1000 {
		t.Errorf("PeakRecords = %d, want 1000", stats.PeakRecords)
	}
}

// A tiny chunk relative to the dataset is the "sort 100 GB in 2 GB" case.
func TestVerySmallChunkStillSorts(t *testing.T) {
	dir := t.TempDir()
	values := randomInts(5000, 3)
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	stats, err := Sort(in, out, Config{ChunkSize: 10, TempDir: dir, MaxMergeWidth: 8})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Runs != 500 {
		t.Errorf("Runs = %d, want 500", stats.Runs)
	}
	if stats.MergePasses < 2 {
		t.Errorf("MergePasses = %d, want multiple passes at width 8", stats.MergePasses)
	}

	got, _ := readInts(out)
	if !sort.IntsAreSorted(got) {
		t.Fatal("output is not sorted")
	}
	if len(got) != len(values) {
		t.Errorf("got %d records, want %d — the merge lost data", len(got), len(values))
	}
}

// ⚠️ Merging thousands of runs at once exhausts file descriptors, so the width
// cap must actually force extra passes.
func TestMergeWidthIsRespected(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, randomInts(2000, 4))
	out := filepath.Join(dir, "out.txt")

	narrow, err := Sort(in, out, Config{ChunkSize: 100, TempDir: dir, MaxMergeWidth: 2})
	if err != nil {
		t.Fatal(err)
	}
	wide, err := Sort(in, filepath.Join(dir, "out2.txt"),
		Config{ChunkSize: 100, TempDir: dir, MaxMergeWidth: 100})
	if err != nil {
		t.Fatal(err)
	}

	if narrow.MergePasses <= wide.MergePasses {
		t.Errorf("width 2 used %d passes, width 100 used %d — narrower should need more",
			narrow.MergePasses, wide.MergePasses)
	}
}

func TestSingleRunSkipsMerging(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, randomInts(100, 5))
	out := filepath.Join(dir, "out.txt")

	stats, err := Sort(in, out, Config{ChunkSize: 1000, TempDir: dir, MaxMergeWidth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Runs != 1 {
		t.Errorf("Runs = %d, want 1", stats.Runs)
	}
	if stats.MergePasses != 0 {
		t.Errorf("MergePasses = %d, want 0 — one run needs no merge", stats.MergePasses)
	}

	got, _ := readInts(out)
	if !sort.IntsAreSorted(got) || len(got) != 100 {
		t.Error("single-run output is wrong")
	}
}

func TestEmptyInput(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, nil)
	out := filepath.Join(dir, "out.txt")

	stats, err := Sort(in, out, DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Records != 0 {
		t.Errorf("Records = %d, want 0", stats.Records)
	}
	if got, _ := readInts(out); len(got) != 0 {
		t.Errorf("output has %d records, want 0", len(got))
	}
}

func TestSingleRecord(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, []int{42})
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 10, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}
	if got, _ := readInts(out); !reflect.DeepEqual(got, []int{42}) {
		t.Errorf("got %v, want [42]", got)
	}
}

func TestDuplicatesArePreserved(t *testing.T) {
	dir := t.TempDir()
	values := make([]int, 1000)
	for i := range values {
		values[i] = i % 10 // heavy duplication
	}
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 50, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}

	got, _ := readInts(out)
	if len(got) != 1000 {
		t.Errorf("got %d records, want 1000 — duplicates were dropped", len(got))
	}
	counts := map[int]int{}
	for _, v := range got {
		counts[v]++
	}
	for v := 0; v < 10; v++ {
		if counts[v] != 100 {
			t.Errorf("value %d appears %d times, want 100", v, counts[v])
		}
	}
}

func TestAlreadySortedInput(t *testing.T) {
	dir := t.TempDir()
	values := make([]int, 1000)
	for i := range values {
		values[i] = i
	}
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 100, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}
	got, _ := readInts(out)
	if !reflect.DeepEqual(got, values) {
		t.Error("already-sorted input was corrupted")
	}
}

func TestReverseSortedInput(t *testing.T) {
	dir := t.TempDir()
	values := make([]int, 1000)
	for i := range values {
		values[i] = 1000 - i
	}
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 100, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}
	got, _ := readInts(out)
	if !sort.IntsAreSorted(got) || len(got) != 1000 {
		t.Error("reverse-sorted input produced wrong output")
	}
}

func TestNegativeValues(t *testing.T) {
	dir := t.TempDir()
	values := []int{5, -3, 0, -100, 42, -1}
	in := writeTestInput(t, dir, values)
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 2, TempDir: dir, MaxMergeWidth: 2}); err != nil {
		t.Fatal(err)
	}
	want := []int{-100, -3, -1, 0, 5, 42}
	if got, _ := readInts(out); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTemporaryFilesAreCleanedUp(t *testing.T) {
	dir := t.TempDir()
	in := writeTestInput(t, dir, randomInts(5000, 6))
	out := filepath.Join(dir, "out.txt")

	if _, err := Sort(in, out, Config{ChunkSize: 100, TempDir: dir, MaxMergeWidth: 4}); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temporary file %s was left behind", e.Name())
		}
	}
}

func TestMalformedInputIsRejected(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "bad.txt")
	os.WriteFile(in, []byte("1\n2\nnot-a-number\n4\n"), 0o644)

	if _, err := Sort(in, filepath.Join(dir, "out.txt"), DefaultConfig(dir)); err == nil {
		t.Error("malformed input should produce an error")
	}
}

func TestMissingInputFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Sort(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "out.txt"), DefaultConfig(dir)); err == nil {
		t.Error("a missing input file should produce an error")
	}
}

func BenchmarkSort100k(b *testing.B) {
	dir := b.TempDir()
	values := randomInts(100000, 9)
	path := filepath.Join(dir, "in.txt")
	writeInts(path, values)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Sort(path, filepath.Join(dir, "out.txt"),
			Config{ChunkSize: 10000, TempDir: dir, MaxMergeWidth: 8})
	}
}
