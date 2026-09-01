// Package main implements the write path of an LSM tree: a skip-list memtable
// that flushes to an immutable, sorted SSTable. See Chapter 6.
//
// Why a skip list rather than a balanced tree: it gives O(log n) ordered
// operations with simple, lock-friendly code and no rebalancing, and iterating
// in sorted order is just walking level 0 — which is exactly what flushing needs.
//
// Why this design is write-optimised: every write is an in-memory insert plus a
// sequential log append. There is no random I/O on the write path at all. The
// cost is paid on reads, which may have to consult the memtable and then several
// SSTables — that is read amplification, and it is what compaction and Bloom
// filters exist to contain.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	maxLevel    = 16
	probability = 0.25
)

type skipNode struct {
	key     string
	value   string
	deleted bool // tombstone
	next    []*skipNode
}

// Memtable is a sorted in-memory buffer. Deletes insert a tombstone rather than
// removing the key, because older SSTables may still hold the value; the delete
// only becomes real when compaction drops both.
type Memtable struct {
	mu    sync.RWMutex
	head  *skipNode
	level int
	count int
	bytes int
	rng   *rand.Rand
}

func NewMemtable() *Memtable {
	return &Memtable{
		head:  &skipNode{next: make([]*skipNode, maxLevel)},
		level: 1,
		rng:   rand.New(rand.NewSource(1)),
	}
}

func (m *Memtable) randomLevel() int {
	l := 1
	for l < maxLevel && m.rng.Float64() < probability {
		l++
	}
	return l
}

func (m *Memtable) Put(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insert(key, value, false)
}

func (m *Memtable) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insert(key, "", true)
}

func (m *Memtable) insert(key, value string, deleted bool) {
	update := make([]*skipNode, maxLevel)
	x := m.head
	for i := m.level - 1; i >= 0; i-- {
		for x.next[i] != nil && x.next[i].key < key {
			x = x.next[i]
		}
		update[i] = x
	}

	if next := x.next[0]; next != nil && next.key == key {
		m.bytes += len(value) - len(next.value)
		next.value, next.deleted = value, deleted
		return
	}

	lvl := m.randomLevel()
	if lvl > m.level {
		for i := m.level; i < lvl; i++ {
			update[i] = m.head
		}
		m.level = lvl
	}

	n := &skipNode{key: key, value: value, deleted: deleted, next: make([]*skipNode, lvl)}
	for i := 0; i < lvl; i++ {
		n.next[i] = update[i].next[i]
		update[i].next[i] = n
	}
	m.count++
	m.bytes += len(key) + len(value)
}

// Get returns the value and whether the key is present. A tombstone reports
// found=false but still shadows older SSTables — that distinction is why the
// caller must stop searching once the memtable answers.
func (m *Memtable) Get(key string) (value string, found bool, tombstone bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	x := m.head
	for i := m.level - 1; i >= 0; i-- {
		for x.next[i] != nil && x.next[i].key < key {
			x = x.next[i]
		}
	}
	if n := x.next[0]; n != nil && n.key == key {
		if n.deleted {
			return "", false, true
		}
		return n.value, true, false
	}
	return "", false, false
}

func (m *Memtable) Len() int   { return m.count }
func (m *Memtable) Bytes() int { return m.bytes }

// Entries walks level 0, which is already in sorted order.
func (m *Memtable) Entries() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Entry, 0, m.count)
	for n := m.head.next[0]; n != nil; n = n.next[0] {
		out = append(out, Entry{Key: n.key, Value: n.value, Deleted: n.deleted})
	}
	return out
}

type Entry struct {
	Key     string
	Value   string
	Deleted bool
}

// SSTable is an immutable on-disk sorted run with a sparse index.
//
// Sparse indexing is the space/time trade at the heart of the format: keeping one
// index entry per N keys costs a short sequential scan on lookup but keeps the
// index small enough to stay in memory.
type SSTable struct {
	path string
	// index maps a key to its byte offset, for every indexInterval-th key.
	index    []indexEntry
	minKey   string
	maxKey   string
	count    int
	interval int
}

type indexEntry struct {
	key    string
	offset int64
}

const indexInterval = 16

// Flush writes the memtable to an immutable SSTable. Everything here is a
// sequential write, which is why LSM ingest is fast.
func Flush(m *Memtable, path string) (*SSTable, error) {
	entries := m.Entries()
	if len(entries) == 0 {
		return nil, fmt.Errorf("sstable: refusing to flush an empty memtable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	t := &SSTable{
		path:     path,
		minKey:   entries[0].Key,
		maxKey:   entries[len(entries)-1].Key,
		count:    len(entries),
		interval: indexInterval,
	}

	var offset int64
	header := make([]byte, 9)
	for i, e := range entries {
		if i%indexInterval == 0 {
			t.index = append(t.index, indexEntry{key: e.Key, offset: offset})
		}
		binary.LittleEndian.PutUint32(header[0:], uint32(len(e.Key)))
		binary.LittleEndian.PutUint32(header[4:], uint32(len(e.Value)))
		if e.Deleted {
			header[8] = 1
		} else {
			header[8] = 0
		}
		w.Write(header)
		w.WriteString(e.Key)
		w.WriteString(e.Value)
		offset += int64(9 + len(e.Key) + len(e.Value))
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return t, f.Sync()
}

// Get scans from the nearest index entry at or before key.
func (t *SSTable) Get(key string) (value string, found bool, tombstone bool, err error) {
	if key < t.minKey || key > t.maxKey {
		return "", false, false, nil // range check: cheapest possible rejection
	}

	i := sort.Search(len(t.index), func(i int) bool { return t.index[i].key > key })
	start := int64(0)
	if i > 0 {
		start = t.index[i-1].offset
	}

	f, err := os.Open(t.path)
	if err != nil {
		return "", false, false, err
	}
	defer f.Close()
	if _, err := f.Seek(start, 0); err != nil {
		return "", false, false, err
	}

	r := bufio.NewReader(f)
	header := make([]byte, 9)
	for scanned := 0; scanned <= t.interval; scanned++ {
		if _, err := readFull(r, header); err != nil {
			return "", false, false, nil
		}
		keyLen := binary.LittleEndian.Uint32(header[0:])
		valLen := binary.LittleEndian.Uint32(header[4:])
		deleted := header[8] == 1

		body := make([]byte, keyLen+valLen)
		if _, err := readFull(r, body); err != nil {
			return "", false, false, nil
		}
		k := string(body[:keyLen])
		switch {
		case k == key:
			if deleted {
				return "", false, true, nil
			}
			return string(body[keyLen:]), true, false, nil
		case k > key:
			return "", false, false, nil // sorted, so it cannot appear later
		}
	}
	return "", false, false, nil
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (t *SSTable) Count() int     { return t.count }
func (t *SSTable) IndexSize() int { return len(t.index) }
func (t *SSTable) MinKey() string { return t.minKey }
func (t *SSTable) MaxKey() string { return t.maxKey }

// LSMTree ties the pieces together: writes hit the memtable, and a full memtable
// is flushed to a new SSTable. Reads consult the memtable first, then SSTables
// newest to oldest — the first answer wins, including a tombstone.
type LSMTree struct {
	mu           sync.RWMutex
	mem          *Memtable
	tables       []*SSTable // newest last
	dir          string
	flushBytes   int
	flushCounter int
}

func NewLSMTree(dir string, flushBytes int) *LSMTree {
	return &LSMTree{mem: NewMemtable(), dir: dir, flushBytes: flushBytes}
}

func (l *LSMTree) Put(key, value string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mem.Put(key, value)
	return l.maybeFlush()
}

func (l *LSMTree) Delete(key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.mem.Delete(key)
	return l.maybeFlush()
}

func (l *LSMTree) maybeFlush() error {
	if l.mem.Bytes() < l.flushBytes {
		return nil
	}
	return l.flushLocked()
}

func (l *LSMTree) flushLocked() error {
	if l.mem.Len() == 0 {
		return nil
	}
	path := filepath.Join(l.dir, fmt.Sprintf("sst-%04d.dat", l.flushCounter))
	t, err := Flush(l.mem, path)
	if err != nil {
		return err
	}
	l.flushCounter++
	l.tables = append(l.tables, t)
	l.mem = NewMemtable()
	return nil
}

// Flush forces the memtable to disk.
func (l *LSMTree) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flushLocked()
}

func (l *LSMTree) Get(key string) (string, bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if v, found, tomb := l.mem.Get(key); found || tomb {
		return v, found, nil
	}
	// Newest first: a later SSTable shadows an older one.
	for i := len(l.tables) - 1; i >= 0; i-- {
		v, found, tomb, err := l.tables[i].Get(key)
		if err != nil {
			return "", false, err
		}
		if found || tomb {
			return v, found, nil
		}
	}
	return "", false, nil
}

func (l *LSMTree) Tables() int { return len(l.tables) }

func main() {
	dir, _ := os.MkdirTemp("", "lsm-demo")
	defer os.RemoveAll(dir)

	tree := NewLSMTree(dir, 2000) // flush every ~2 KB so the demo produces several runs

	for i := 0; i < 500; i++ {
		tree.Put(fmt.Sprintf("key:%04d", i), strings.Repeat("v", 10))
	}
	tree.Put("key:0042", "updated-value")
	tree.Delete("key:0100")
	tree.Flush()

	fmt.Printf("wrote 500 keys, flushed into %d SSTables\n\n", tree.Tables())

	for _, k := range []string{"key:0000", "key:0042", "key:0100", "key:0499", "key:9999"} {
		v, found, _ := tree.Get(k)
		if found {
			fmt.Printf("  %s = %q\n", k, v)
		} else {
			fmt.Printf("  %s = <not found>\n", k)
		}
	}

	fmt.Println("\nkey:0042 shows the newest value; key:0100 is shadowed by a tombstone")
	fmt.Println("a read may touch several SSTables — that is read amplification,")
	fmt.Println("and it is what compaction and Bloom filters exist to bound")
}
