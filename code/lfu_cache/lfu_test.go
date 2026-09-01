package main

import (
	"strconv"
	"testing"
)

func TestEvictsLeastFrequentlyUsed(t *testing.T) {
	c := NewLFUCache(3)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")
	for i := 0; i < 5; i++ {
		c.Get("a")
	}
	c.Get("b")
	c.Put("d", "4")

	if _, ok := c.Get("c"); ok {
		t.Error("c had the lowest frequency and should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a was the hottest key and should have survived")
	}
}

// Within one frequency bucket, LFU degenerates to LRU.
func TestTiesBrokenByRecency(t *testing.T) {
	c := NewLFUCache(2)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Get("a") // both now at freq 2, but a is more recent
	c.Get("b")
	c.Get("a")
	c.Get("b")
	c.Get("a") // a: freq 4, b: freq 3

	c.Put("c", "3")
	if _, ok := c.Get("b"); ok {
		t.Error("b had lower frequency and should have been evicted")
	}
}

func TestFrequencyIncrements(t *testing.T) {
	c := NewLFUCache(2)
	c.Put("a", "1")
	if f, _ := c.Freq("a"); f != 1 {
		t.Errorf("freq after Put = %d, want 1", f)
	}
	c.Get("a")
	c.Get("a")
	if f, _ := c.Freq("a"); f != 3 {
		t.Errorf("freq after 2 Gets = %d, want 3", f)
	}
}

func TestPutExistingPromotesWithoutGrowing(t *testing.T) {
	c := NewLFUCache(2)
	c.Put("a", "1")
	c.Put("a", "2")

	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
	if f, _ := c.Freq("a"); f != 2 {
		t.Errorf("freq = %d, want 2", f)
	}
}

func TestNeverExceedsCapacity(t *testing.T) {
	c := NewLFUCache(10)
	for i := 0; i < 1000; i++ {
		c.Put(strconv.Itoa(i), "v")
		if c.Len() > 10 {
			t.Fatalf("Len = %d, exceeded capacity", c.Len())
		}
	}
}

// The buckets list must not leak once a frequency has no members.
func TestEmptyBucketsAreReclaimed(t *testing.T) {
	c := NewLFUCache(1)
	c.Put("a", "1")
	for i := 0; i < 100; i++ {
		c.Get("a")
	}
	n := 0
	for b := c.buckets.next; b != c.buckets; b = b.next {
		n++
	}
	if n != 1 {
		t.Errorf("live buckets = %d, want 1", n)
	}
}
