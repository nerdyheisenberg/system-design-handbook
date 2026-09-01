// Package main implements a Merkle tree for anti-entropy. See Chapter 9.
//
// The problem: two replicas each hold a million keys and you need to find the
// handful that differ, without shipping a million keys across the network.
//
// The answer: hash the leaves, hash pairs of hashes up to a single root. Equal
// roots mean the datasets are identical — one hash comparison for the whole set.
// Unequal roots mean you recurse only into subtrees whose hashes differ, so
// finding d differing keys among n costs O(d log n) comparisons instead of O(n).
//
// This is how Cassandra and DynamoDB repair replicas, and how Git, IPFS,
// BitTorrent and blockchains verify content.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

type Node struct {
	Hash        []byte
	Left, Right *Node
	Key         string // leaves only
}

func (n *Node) IsLeaf() bool { return n.Left == nil && n.Right == nil }

type MerkleTree struct {
	Root   *Node
	leaves []*Node
}

func hashLeaf(key, value string) []byte {
	// Leaf and internal hashes are domain-separated so an attacker cannot pass
	// off an internal node as a leaf (the second-preimage attack on naive Merkle
	// trees).
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write([]byte(key))
	h.Write([]byte{0x1f})
	h.Write([]byte(value))
	return h.Sum(nil)
}

func hashInternal(left, right []byte) []byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(left)
	h.Write(right)
	return h.Sum(nil)
}

// Build constructs a tree over the data. Keys are sorted first so that two
// replicas holding the same data always produce the same tree regardless of
// insertion order — without this the roots would differ spuriously.
func Build(data map[string]string) *MerkleTree {
	if len(data) == 0 {
		return &MerkleTree{}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	leaves := make([]*Node, len(keys))
	for i, k := range keys {
		leaves[i] = &Node{Hash: hashLeaf(k, data[k]), Key: k}
	}

	level := leaves
	for len(level) > 1 {
		var next []*Node
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				// Odd node out: promote it rather than duplicating it, which
				// avoids the CVE-2012-2459 style ambiguity where two different
				// leaf sets yield the same root.
				next = append(next, level[i])
				continue
			}
			next = append(next, &Node{
				Hash:  hashInternal(level[i].Hash, level[i+1].Hash),
				Left:  level[i],
				Right: level[i+1],
			})
		}
		level = next
	}
	return &MerkleTree{Root: level[0], leaves: leaves}
}

func (t *MerkleTree) RootHash() string {
	if t.Root == nil {
		return ""
	}
	return hex.EncodeToString(t.Root.Hash)
}

func (t *MerkleTree) Leaves() int { return len(t.leaves) }

// Diff returns the keys whose leaf hashes differ between the two trees, plus
// keys present in only one. It descends only into mismatched subtrees.
func Diff(a, b *MerkleTree) []string {
	if a.Root == nil && b.Root == nil {
		return nil
	}
	if a.Root == nil {
		return keysOf(b.leaves)
	}
	if b.Root == nil {
		return keysOf(a.leaves)
	}

	seen := map[string]bool{}
	var walk func(x, y *Node)
	walk = func(x, y *Node) {
		switch {
		case x == nil && y == nil:
			return
		case x == nil:
			for _, k := range collect(y) {
				seen[k] = true
			}
			return
		case y == nil:
			for _, k := range collect(x) {
				seen[k] = true
			}
			return
		}
		// ⭐ The pruning step: identical subtrees are skipped entirely.
		if equalHash(x.Hash, y.Hash) {
			return
		}
		if x.IsLeaf() || y.IsLeaf() {
			for _, k := range append(collect(x), collect(y)...) {
				seen[k] = true
			}
			return
		}
		walk(x.Left, y.Left)
		walk(x.Right, y.Right)
	}
	walk(a.Root, b.Root)

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CompareCost reports how many node comparisons Diff performed, to demonstrate
// that it is logarithmic in the dataset rather than linear.
func CompareCost(a, b *MerkleTree) int {
	count := 0
	var walk func(x, y *Node)
	walk = func(x, y *Node) {
		count++
		if x == nil || y == nil {
			return
		}
		if equalHash(x.Hash, y.Hash) {
			return
		}
		if x.IsLeaf() || y.IsLeaf() {
			return
		}
		walk(x.Left, y.Left)
		walk(x.Right, y.Right)
	}
	if a.Root != nil || b.Root != nil {
		walk(a.Root, b.Root)
	}
	return count
}

func collect(n *Node) []string {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []string{n.Key}
	}
	return append(collect(n.Left), collect(n.Right)...)
}

func keysOf(leaves []*Node) []string {
	out := make([]string, len(leaves))
	for i, l := range leaves {
		out[i] = l.Key
	}
	sort.Strings(out)
	return out
}

func equalHash(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func main() {
	const n = 100000
	replicaA := make(map[string]string, n)
	for i := 0; i < n; i++ {
		replicaA[fmt.Sprintf("key:%06d", i)] = fmt.Sprintf("value:%d", i)
	}

	replicaB := make(map[string]string, n)
	for k, v := range replicaA {
		replicaB[k] = v
	}
	// Three replicas drifted apart.
	replicaB["key:000042"] = "STALE"
	replicaB["key:050000"] = "STALE"
	delete(replicaB, "key:099999")

	a, b := Build(replicaA), Build(replicaB)

	fmt.Printf("replica A root: %s\n", a.RootHash()[:16])
	fmt.Printf("replica B root: %s\n", b.RootHash()[:16])
	fmt.Println("roots differ, so the replicas are out of sync")
	fmt.Println()

	diff := Diff(a, b)
	fmt.Printf("keys needing repair: %v\n", diff)
	fmt.Printf("\nfound %d differences among %d keys using %d node comparisons\n",
		len(diff), n, CompareCost(a, b))
	fmt.Printf("a naive full comparison would have been %d\n", n)

	identical := Build(replicaA)
	fmt.Printf("\nidentical replicas: %d comparison, diff=%v\n",
		CompareCost(a, identical), Diff(a, identical))
}
