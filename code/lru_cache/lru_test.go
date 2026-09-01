package main

import (
	"reflect"
	"strconv"
	"testing"
)

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	c := NewLRUCache(3)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")
	c.Get("a")
	c.Put("d", "4")

	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Errorf("%s should still be present", k)
		}
	}
}

func TestGetPromotesToFront(t *testing.T) {
	c := NewLRUCache(3)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")
	c.Get("a")

	want := []string{"a", "c", "b"}
	if got := c.keysMRUFirst(); !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestPutExistingUpdatesWithoutGrowing(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("a", "1")
	c.Put("a", "2")

	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
	if v, _ := c.Get("a"); v != "2" {
		t.Errorf("value = %q, want \"2\"", v)
	}
}

func TestNeverExceedsCapacity(t *testing.T) {
	c := NewLRUCache(10)
	for i := 0; i < 1000; i++ {
		c.Put(strconv.Itoa(i), "v")
		if c.Len() > 10 {
			t.Fatalf("Len = %d, exceeded capacity", c.Len())
		}
	}
}

func TestHitRatio(t *testing.T) {
	c := NewLRUCache(2)
	c.Put("a", "1")
	c.Get("a")     // hit
	c.Get("miss1") // miss
	c.Get("miss2") // miss

	if got := c.HitRatio(); got < 0.33 || got > 0.34 {
		t.Errorf("HitRatio = %f, want ~0.333", got)
	}
}

func BenchmarkPutGet(b *testing.B) {
	c := NewLRUCache(1000)
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i%len(keys)]
		if _, ok := c.Get(k); !ok {
			c.Put(k, "v")
		}
	}
}
