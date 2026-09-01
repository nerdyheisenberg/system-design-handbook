package main

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"
)

func TestMemtablePutGet(t *testing.T) {
	m := NewMemtable()
	m.Put("b", "2")
	m.Put("a", "1")
	m.Put("c", "3")

	for k, want := range map[string]string{"a": "1", "b": "2", "c": "3"} {
		v, found, _ := m.Get(k)
		if !found || v != want {
			t.Errorf("Get(%q) = %q,%v want %q,true", k, v, found, want)
		}
	}
	if _, found, _ := m.Get("missing"); found {
		t.Error("absent key reported as found")
	}
}

func TestMemtableOverwrite(t *testing.T) {
	m := NewMemtable()
	m.Put("k", "old")
	m.Put("k", "new")

	if v, _, _ := m.Get("k"); v != "new" {
		t.Errorf("value = %q, want \"new\"", v)
	}
	if m.Len() != 1 {
		t.Errorf("Len = %d, want 1 — overwrite must not add a node", m.Len())
	}
}

// Deletes must leave a tombstone, not remove the key: older SSTables may still
// hold a value that the tombstone has to shadow.
func TestDeleteLeavesTombstone(t *testing.T) {
	m := NewMemtable()
	m.Put("k", "v")
	m.Delete("k")

	v, found, tomb := m.Get("k")
	if found {
		t.Error("deleted key reported as found")
	}
	if !tomb {
		t.Error("delete must record a tombstone, not remove the entry")
	}
	if v != "" {
		t.Errorf("value = %q, want empty", v)
	}
}

// Flushing depends on this: level 0 must already be in key order.
func TestEntriesAreSorted(t *testing.T) {
	m := NewMemtable()
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		m.Put(fmt.Sprintf("key:%05d", rng.Intn(10000)), "v")
	}

	entries := m.Entries()
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key }) {
		t.Fatal("Entries() is not sorted")
	}
	if len(entries) != m.Len() {
		t.Errorf("Entries returned %d, Len is %d", len(entries), m.Len())
	}
}

func TestSkipListHandlesManyKeys(t *testing.T) {
	m := NewMemtable()
	const n = 10000
	for i := 0; i < n; i++ {
		m.Put(fmt.Sprintf("key:%06d", i), fmt.Sprintf("val:%d", i))
	}
	if m.Len() != n {
		t.Fatalf("Len = %d, want %d", m.Len(), n)
	}
	for i := 0; i < n; i += 97 {
		want := fmt.Sprintf("val:%d", i)
		if v, found, _ := m.Get(fmt.Sprintf("key:%06d", i)); !found || v != want {
			t.Fatalf("Get(key:%06d) = %q,%v", i, v, found)
		}
	}
}

func TestFlushRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewMemtable()
	for i := 0; i < 100; i++ {
		m.Put(fmt.Sprintf("key:%03d", i), fmt.Sprintf("value:%d", i))
	}

	sst, err := Flush(m, filepath.Join(dir, "test.sst"))
	if err != nil {
		t.Fatal(err)
	}
	if sst.Count() != 100 {
		t.Errorf("Count = %d, want 100", sst.Count())
	}
	if sst.MinKey() != "key:000" || sst.MaxKey() != "key:099" {
		t.Errorf("range = [%s, %s]", sst.MinKey(), sst.MaxKey())
	}

	for i := 0; i < 100; i++ {
		v, found, _, err := sst.Get(fmt.Sprintf("key:%03d", i))
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("value:%d", i)
		if !found || v != want {
			t.Fatalf("Get(key:%03d) = %q,%v want %q", i, v, found, want)
		}
	}
}

// The sparse index is the space/time trade: ~1/16th the entries, one short scan.
func TestSparseIndexIsSparse(t *testing.T) {
	dir := t.TempDir()
	m := NewMemtable()
	for i := 0; i < 1000; i++ {
		m.Put(fmt.Sprintf("key:%04d", i), "v")
	}
	sst, _ := Flush(m, filepath.Join(dir, "t.sst"))

	want := 1000/indexInterval + 1
	if sst.IndexSize() != want {
		t.Errorf("index entries = %d, want %d", sst.IndexSize(), want)
	}
	if sst.IndexSize() >= sst.Count() {
		t.Error("index is not sparse")
	}
}

func TestSSTableRangeCheckRejectsOutOfRange(t *testing.T) {
	dir := t.TempDir()
	m := NewMemtable()
	m.Put("m", "1")
	m.Put("n", "2")
	sst, _ := Flush(m, filepath.Join(dir, "t.sst"))

	for _, k := range []string{"a", "z"} {
		if _, found, _, _ := sst.Get(k); found {
			t.Errorf("Get(%q) found something outside [%s,%s]", k, sst.MinKey(), sst.MaxKey())
		}
	}
}

func TestSSTablePreservesTombstones(t *testing.T) {
	dir := t.TempDir()
	m := NewMemtable()
	m.Put("a", "1")
	m.Delete("b")
	m.Put("c", "3")
	sst, _ := Flush(m, filepath.Join(dir, "t.sst"))

	_, found, tomb, _ := sst.Get("b")
	if found {
		t.Error("tombstoned key reported as found")
	}
	if !tomb {
		t.Error("tombstone was lost in the flush — deleted data would resurrect")
	}
}

func TestFlushEmptyMemtableFails(t *testing.T) {
	if _, err := Flush(NewMemtable(), filepath.Join(t.TempDir(), "e.sst")); err == nil {
		t.Error("flushing an empty memtable should fail")
	}
}

// A newer SSTable must shadow an older one holding the same key.
func TestNewerTableShadowsOlder(t *testing.T) {
	dir := t.TempDir()
	tree := NewLSMTree(dir, 1)

	tree.Put("k", "v1")
	tree.Flush()
	tree.Put("k", "v2")
	tree.Flush()

	if tree.Tables() != 2 {
		t.Fatalf("Tables = %d, want 2", tree.Tables())
	}
	v, found, err := tree.Get("k")
	if err != nil {
		t.Fatal(err)
	}
	if !found || v != "v2" {
		t.Errorf("Get = %q,%v want \"v2\",true", v, found)
	}
}

// A tombstone in a newer table must hide a value in an older one.
func TestTombstoneShadowsOlderValue(t *testing.T) {
	dir := t.TempDir()
	tree := NewLSMTree(dir, 1)

	tree.Put("k", "v")
	tree.Flush()
	tree.Delete("k")
	tree.Flush()

	if _, found, _ := tree.Get("k"); found {
		t.Error("deleted key resurfaced from an older SSTable")
	}
}

func TestTreeReadsAcrossMemtableAndTables(t *testing.T) {
	dir := t.TempDir()
	tree := NewLSMTree(dir, 500)

	for i := 0; i < 300; i++ {
		tree.Put(fmt.Sprintf("key:%04d", i), fmt.Sprintf("v%d", i))
	}
	if tree.Tables() == 0 {
		t.Fatal("expected automatic flushes")
	}

	for i := 0; i < 300; i++ {
		v, found, err := tree.Get(fmt.Sprintf("key:%04d", i))
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("v%d", i); !found || v != want {
			t.Fatalf("key:%04d = %q,%v want %q", i, v, found, want)
		}
	}
}

func TestTreeMissingKey(t *testing.T) {
	dir := t.TempDir()
	tree := NewLSMTree(dir, 100)
	for i := 0; i < 200; i++ {
		tree.Put(fmt.Sprintf("key:%d", i), "v")
	}
	tree.Flush()

	if _, found, _ := tree.Get("absent"); found {
		t.Error("absent key reported as found")
	}
}

func BenchmarkMemtablePut(b *testing.B) {
	m := NewMemtable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Put(fmt.Sprintf("key:%08d", i), "value")
	}
}

func BenchmarkMemtableGet(b *testing.B) {
	m := NewMemtable()
	for i := 0; i < 100000; i++ {
		m.Put(fmt.Sprintf("key:%08d", i), "value")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get(fmt.Sprintf("key:%08d", i%100000))
	}
}
