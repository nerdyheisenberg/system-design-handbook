// Package main implements a B+tree, the structure behind almost every relational
// database index. See Chapter 6.
//
// Two properties matter and both come from the same design choice — values live
// only in leaves, and leaves are chained:
//
//  1. Internal nodes hold only keys, so they fan out very wide. A node sized to
//     one 4 KB page holds hundreds of keys, so a tree over a billion rows is
//     three or four levels deep. That is three or four disk reads for any lookup.
//
//  2. Range scans walk the leaf chain sequentially instead of traversing the tree
//     repeatedly, which is why `WHERE x BETWEEN a AND b` is cheap.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// order is the maximum number of children per internal node. Real engines derive
// this from the page size; 4 keeps the demo output readable.
const order = 4

type node struct {
	leaf     bool
	keys     []string
	values   []string // leaves only, parallel to keys
	children []*node  // internal only, len(children) == len(keys)+1
	next     *node    // leaves only: the sequential scan chain
}

type BPlusTree struct {
	root  *node
	size  int
	order int
}

func New() *BPlusTree {
	return &BPlusTree{root: &node{leaf: true}, order: order}
}

func (t *BPlusTree) Len() int { return t.size }

func (t *BPlusTree) Height() int {
	h := 1
	for n := t.root; !n.leaf; n = n.children[0] {
		h++
	}
	return h
}

func (t *BPlusTree) Get(key string) (string, bool) {
	n := t.root
	for !n.leaf {
		n = n.children[n.childIndex(key)]
	}
	i := sort.SearchStrings(n.keys, key)
	if i < len(n.keys) && n.keys[i] == key {
		return n.values[i], true
	}
	return "", false
}

// childIndex returns the subtree to follow for key. Keys equal to a separator go
// right, which keeps the leaf-level invariant that separator keys are the minimum
// of their right subtree.
func (n *node) childIndex(key string) int {
	i := sort.SearchStrings(n.keys, key)
	if i < len(n.keys) && n.keys[i] == key {
		i++
	}
	return i
}

func (t *BPlusTree) Put(key, value string) {
	if promoted, right := t.insert(t.root, key, value); right != nil {
		// The root split, so the tree grows by one level. This is the only way a
		// B+tree gets taller, which is what keeps it balanced by construction.
		t.root = &node{
			keys:     []string{promoted},
			children: []*node{t.root, right},
		}
	}
}

// insert returns a promoted separator key and a new right sibling when n split.
func (t *BPlusTree) insert(n *node, key, value string) (string, *node) {
	if n.leaf {
		i := sort.SearchStrings(n.keys, key)
		if i < len(n.keys) && n.keys[i] == key {
			n.values[i] = value
			return "", nil
		}
		n.keys = insertStringAt(n.keys, i, key)
		n.values = insertStringAt(n.values, i, value)
		t.size++

		if len(n.keys) < t.order {
			return "", nil
		}
		return t.splitLeaf(n)
	}

	i := n.childIndex(key)
	promoted, right := t.insert(n.children[i], key, value)
	if right == nil {
		return "", nil
	}

	n.keys = insertStringAt(n.keys, i, promoted)
	n.children = insertNodeAt(n.children, i+1, right)
	if len(n.keys) < t.order {
		return "", nil
	}
	return t.splitInternal(n)
}

// splitLeaf copies the median key up. The key stays in the leaf too, because
// leaves must hold every key in a B+tree.
func (t *BPlusTree) splitLeaf(n *node) (string, *node) {
	mid := len(n.keys) / 2
	right := &node{
		leaf:   true,
		keys:   append([]string{}, n.keys[mid:]...),
		values: append([]string{}, n.values[mid:]...),
		next:   n.next,
	}
	n.keys = n.keys[:mid]
	n.values = n.values[:mid]
	n.next = right
	return right.keys[0], right
}

// splitInternal moves the median key up; unlike a leaf split it is removed from
// both halves, since internal nodes only route.
func (t *BPlusTree) splitInternal(n *node) (string, *node) {
	mid := len(n.keys) / 2
	promoted := n.keys[mid]
	right := &node{
		keys:     append([]string{}, n.keys[mid+1:]...),
		children: append([]*node{}, n.children[mid+1:]...),
	}
	n.keys = n.keys[:mid]
	n.children = n.children[:mid+1]
	return promoted, right
}

// Range returns all pairs with start <= key <= end by walking the leaf chain.
// This is the operation that makes B+trees the default for databases: one
// descent, then a sequential scan.
func (t *BPlusTree) Range(start, end string) []string {
	n := t.root
	for !n.leaf {
		n = n.children[n.childIndex(start)]
	}

	var out []string
	for ; n != nil; n = n.next {
		for i, k := range n.keys {
			if k < start {
				continue
			}
			if k > end {
				return out
			}
			out = append(out, k+"="+n.values[i])
		}
	}
	return out
}

// Keys returns every key in sorted order via the leaf chain.
func (t *BPlusTree) Keys() []string {
	n := t.root
	for !n.leaf {
		n = n.children[0]
	}
	var out []string
	for ; n != nil; n = n.next {
		out = append(out, n.keys...)
	}
	return out
}

// LeafChainIntact verifies the leaves are linked and globally sorted. A broken
// chain is silent until a range scan returns wrong results, so it is worth
// asserting.
func (t *BPlusTree) LeafChainIntact() bool {
	keys := t.Keys()
	return sort.StringsAreSorted(keys) && len(keys) == t.size
}

func insertStringAt(s []string, i int, v string) []string {
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

func insertNodeAt(s []*node, i int, v *node) []*node {
	s = append(s, nil)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

func (t *BPlusTree) String() string {
	var sb strings.Builder
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		sb.WriteString(strings.Repeat("  ", depth))
		if n.leaf {
			sb.WriteString("leaf ")
		} else {
			sb.WriteString("node ")
		}
		sb.WriteString(fmt.Sprint(n.keys))
		sb.WriteString("\n")
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	walk(t.root, 0)
	return sb.String()
}

func main() {
	t := New()
	for _, k := range []string{"d", "a", "f", "b", "h", "c", "g", "e", "j", "i"} {
		t.Put(k, strings.ToUpper(k))
	}

	fmt.Printf("inserted %d keys, tree height %d\n\n", t.Len(), t.Height())
	fmt.Print(t)

	fmt.Println("\nsorted keys via the leaf chain:", t.Keys())
	fmt.Println("range scan c..g:", t.Range("c", "g"))

	// Height grows logarithmically, which is the whole point.
	big := New()
	for i := 0; i < 100000; i++ {
		big.Put(fmt.Sprintf("key:%08d", i), "v")
	}
	fmt.Printf("\n100,000 keys at order %d: height %d\n", order, big.Height())
	fmt.Println("with a realistic order of ~200 (a 4 KB page), a billion rows")
	fmt.Println("would be 4 levels deep — 4 page reads for any lookup")
}
