// Package main implements a consistent hash ring with virtual nodes. See Chapter 23.
//
// The problem it solves: with hash(key) % N, changing N remaps almost every key.
// Going from 4 nodes to 5 moves ~80% of keys, which means an 80% cache miss rate
// the moment you add capacity. Consistent hashing moves only ~1/N of keys.
package main

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
)

// DefaultVirtualNodes is the replica count per physical node. Below ~100 the
// distribution is visibly uneven; 150 gives roughly +/-5% balance.
const DefaultVirtualNodes = 150

type Ring struct {
	virtualNodes int
	// positions holds every virtual node's hash, kept sorted for binary search.
	positions []uint32
	owners    map[uint32]string
	nodes     map[string]bool
}

func NewRing(virtualNodes int) *Ring {
	if virtualNodes <= 0 {
		virtualNodes = DefaultVirtualNodes
	}
	return &Ring{
		virtualNodes: virtualNodes,
		owners:       make(map[uint32]string),
		nodes:        make(map[string]bool),
	}
}

func hashKey(s string) uint32 {
	sum := sha1.Sum([]byte(s))
	return binary.BigEndian.Uint32(sum[:4])
}

func (r *Ring) Add(node string) {
	if r.nodes[node] {
		return
	}
	r.nodes[node] = true
	for i := 0; i < r.virtualNodes; i++ {
		p := hashKey(node + "#" + strconv.Itoa(i))
		r.positions = append(r.positions, p)
		r.owners[p] = node
	}
	sort.Slice(r.positions, func(i, j int) bool { return r.positions[i] < r.positions[j] })
}

func (r *Ring) Remove(node string) {
	if !r.nodes[node] {
		return
	}
	delete(r.nodes, node)
	kept := r.positions[:0]
	for _, p := range r.positions {
		if r.owners[p] == node {
			delete(r.owners, p)
			continue
		}
		kept = append(kept, p)
	}
	r.positions = kept
}

// Get returns the node owning key: the first virtual node clockwise from it.
func (r *Ring) Get(key string) (string, bool) {
	if len(r.positions) == 0 {
		return "", false
	}
	h := hashKey(key)
	i := sort.Search(len(r.positions), func(i int) bool { return r.positions[i] >= h })
	if i == len(r.positions) {
		i = 0 // wrap around the ring
	}
	return r.owners[r.positions[i]], true
}

// GetN returns the n distinct physical nodes clockwise from key, for replication.
// Distinctness matters: consecutive ring positions often belong to the same machine.
func (r *Ring) GetN(key string, n int) []string {
	if len(r.positions) == 0 || n <= 0 {
		return nil
	}
	if n > len(r.nodes) {
		n = len(r.nodes)
	}
	h := hashKey(key)
	start := sort.Search(len(r.positions), func(i int) bool { return r.positions[i] >= h })

	seen := make(map[string]bool, n)
	out := make([]string, 0, n)
	for i := 0; i < len(r.positions) && len(out) < n; i++ {
		owner := r.owners[r.positions[(start+i)%len(r.positions)]]
		if !seen[owner] {
			seen[owner] = true
			out = append(out, owner)
		}
	}
	return out
}

func (r *Ring) Nodes() int { return len(r.nodes) }

// Distribution counts how many of the given keys land on each node.
func (r *Ring) Distribution(keys []string) map[string]int {
	counts := make(map[string]int, len(r.nodes))
	for n := range r.nodes {
		counts[n] = 0
	}
	for _, k := range keys {
		if n, ok := r.Get(k); ok {
			counts[n]++
		}
	}
	return counts
}

func main() {
	keys := make([]string, 100000)
	for i := range keys {
		keys[i] = "key:" + strconv.Itoa(i)
	}

	r := NewRing(DefaultVirtualNodes)
	for _, n := range []string{"node-a", "node-b", "node-c", "node-d"} {
		r.Add(n)
	}

	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k], _ = r.Get(k)
	}

	fmt.Println("distribution across 4 nodes:")
	for _, n := range []string{"node-a", "node-b", "node-c", "node-d"} {
		c := r.Distribution(keys)[n]
		fmt.Printf("  %-7s %6d  (%.1f%%)\n", n, c, 100*float64(c)/float64(len(keys)))
	}

	r.Add("node-e")
	moved := 0
	for _, k := range keys {
		if now, _ := r.Get(k); now != before[k] {
			moved++
		}
	}
	fmt.Printf("\nafter adding a 5th node, %.1f%% of keys moved (ideal is 1/5 = 20%%)\n",
		100*float64(moved)/float64(len(keys)))
	fmt.Printf("with hash%%N it would have been ~80%%\n")

	fmt.Println("\nreplicas for \"user:42\":", r.GetN("user:42", 3))
}
