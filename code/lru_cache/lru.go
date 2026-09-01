// Package main implements an O(1) LRU cache: hash map for lookup, doubly-linked
// list for recency ordering. See Chapter 11.
package main

import "fmt"

type entry struct {
	key, value string
	prev, next *entry
}

// LRUCache evicts the least recently used entry when capacity is exceeded.
// Not safe for concurrent use; wrap in a sync.Mutex or shard by key hash.
type LRUCache struct {
	capacity int
	items    map[string]*entry
	// head.next is the most recently used, tail.prev the least.
	head, tail *entry
	hits       int
	misses     int
}

func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		panic("lru: capacity must be positive")
	}
	head, tail := &entry{}, &entry{}
	head.next, tail.prev = tail, head
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*entry, capacity),
		head:     head,
		tail:     tail,
	}
}

func (c *LRUCache) Get(key string) (string, bool) {
	e, ok := c.items[key]
	if !ok {
		c.misses++
		return "", false
	}
	c.hits++
	c.unlink(e)
	c.pushFront(e)
	return e.value, true
}

func (c *LRUCache) Put(key, value string) {
	if e, ok := c.items[key]; ok {
		e.value = value
		c.unlink(e)
		c.pushFront(e)
		return
	}
	if len(c.items) >= c.capacity {
		c.evict()
	}
	e := &entry{key: key, value: value}
	c.items[key] = e
	c.pushFront(e)
}

func (c *LRUCache) Len() int { return len(c.items) }

// HitRatio is the number that determines how much load reaches the database.
func (c *LRUCache) HitRatio() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

func (c *LRUCache) evict() {
	lru := c.tail.prev
	if lru == c.head {
		return
	}
	c.unlink(lru)
	delete(c.items, lru.key)
}

func (c *LRUCache) unlink(e *entry) {
	e.prev.next = e.next
	e.next.prev = e.prev
}

func (c *LRUCache) pushFront(e *entry) {
	e.next = c.head.next
	e.prev = c.head
	c.head.next.prev = e
	c.head.next = e
}

// keysMRUFirst returns keys ordered most-recently-used first. Test helper.
func (c *LRUCache) keysMRUFirst() []string {
	var out []string
	for e := c.head.next; e != c.tail; e = e.next {
		out = append(out, e.key)
	}
	return out
}

func main() {
	c := NewLRUCache(3)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")
	c.Get("a")      // a becomes most recently used
	c.Put("d", "4") // evicts b, the least recently used

	fmt.Println("order (MRU first):", c.keysMRUFirst())
	for _, k := range []string{"a", "b", "c", "d"} {
		v, ok := c.Get(k)
		fmt.Printf("  %s -> %q present=%v\n", k, v, ok)
	}
	fmt.Printf("hit ratio: %.2f\n", c.HitRatio())
}
