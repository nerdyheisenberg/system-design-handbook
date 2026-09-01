// Package main implements an O(1) LFU cache using frequency buckets, following
// Shah/Matani/Mittal. See Chapter 11.
//
// The trick that makes eviction O(1): frequency lists are kept in a doubly-linked
// list of buckets in ascending frequency order, so the minimum frequency bucket is
// always the head. Incrementing a key moves it to the adjacent bucket, never a scan.
package main

import "fmt"

type lfuNode struct {
	key, value string
	bucket     *freqBucket
	prev, next *lfuNode
}

type freqBucket struct {
	freq       int
	head, tail *lfuNode // sentinels; head.next is most recently added
	prev, next *freqBucket
}

// LFUCache evicts the least frequently used entry, breaking ties by least
// recently used within the same frequency.
type LFUCache struct {
	capacity int
	items    map[string]*lfuNode
	// buckets.next is the lowest-frequency bucket, i.e. the eviction candidate.
	buckets *freqBucket
}

func NewLFUCache(capacity int) *LFUCache {
	if capacity <= 0 {
		panic("lfu: capacity must be positive")
	}
	sentinel := &freqBucket{freq: 0}
	sentinel.prev, sentinel.next = sentinel, sentinel
	return &LFUCache{
		capacity: capacity,
		items:    make(map[string]*lfuNode, capacity),
		buckets:  sentinel,
	}
}

func newBucket(freq int) *freqBucket {
	b := &freqBucket{freq: freq, head: &lfuNode{}, tail: &lfuNode{}}
	b.head.next, b.tail.prev = b.tail, b.head
	return b
}

func (b *freqBucket) empty() bool { return b.head.next == b.tail }

func (b *freqBucket) pushFront(n *lfuNode) {
	n.next, n.prev = b.head.next, b.head
	b.head.next.prev = n
	b.head.next = n
	n.bucket = b
}

func (b *freqBucket) remove(n *lfuNode) {
	n.prev.next = n.next
	n.next.prev = n.prev
}

func (c *LFUCache) Get(key string) (string, bool) {
	n, ok := c.items[key]
	if !ok {
		return "", false
	}
	c.promote(n)
	return n.value, true
}

func (c *LFUCache) Put(key, value string) {
	if n, ok := c.items[key]; ok {
		n.value = value
		c.promote(n)
		return
	}
	if len(c.items) >= c.capacity {
		c.evict()
	}
	first := c.buckets.next
	if first == c.buckets || first.freq != 1 {
		first = c.insertBucketAfter(c.buckets, 1)
	}
	n := &lfuNode{key: key, value: value}
	first.pushFront(n)
	c.items[key] = n
}

func (c *LFUCache) Len() int { return len(c.items) }

// Freq reports an entry's access count. Test and demo helper.
func (c *LFUCache) Freq(key string) (int, bool) {
	n, ok := c.items[key]
	if !ok {
		return 0, false
	}
	return n.bucket.freq, true
}

// promote moves n into the bucket for freq+1, creating it if needed, and drops
// the old bucket when it empties. All pointer work, so O(1).
func (c *LFUCache) promote(n *lfuNode) {
	old := n.bucket
	next := old.next
	if next == c.buckets || next.freq != old.freq+1 {
		next = c.insertBucketAfter(old, old.freq+1)
	}
	old.remove(n)
	next.pushFront(n)
	if old.empty() {
		c.removeBucket(old)
	}
}

func (c *LFUCache) evict() {
	lowest := c.buckets.next
	if lowest == c.buckets {
		return
	}
	victim := lowest.tail.prev // least recently used within this frequency
	lowest.remove(victim)
	delete(c.items, victim.key)
	if lowest.empty() {
		c.removeBucket(lowest)
	}
}

func (c *LFUCache) insertBucketAfter(after *freqBucket, freq int) *freqBucket {
	b := newBucket(freq)
	b.prev, b.next = after, after.next
	after.next.prev = b
	after.next = b
	return b
}

func (c *LFUCache) removeBucket(b *freqBucket) {
	b.prev.next = b.next
	b.next.prev = b.prev
}

func main() {
	c := NewLFUCache(3)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")

	// Make "a" hot and "b" warm; "c" stays at frequency 1.
	for i := 0; i < 5; i++ {
		c.Get("a")
	}
	c.Get("b")

	c.Put("d", "4") // evicts c, the least frequently used

	for _, k := range []string{"a", "b", "c", "d"} {
		if f, ok := c.Freq(k); ok {
			fmt.Printf("  %s present, freq=%d\n", k, f)
		} else {
			fmt.Printf("  %s evicted\n", k)
		}
	}
}
