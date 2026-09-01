package main

import (
	"sync"
	"testing"
	"time"
)

// fakeClock makes these tests deterministic; sleeping in tests is flaky and slow.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestTokenBucket(capacity int, rate float64) (*TokenBucket, *fakeClock) {
	c := newFakeClock()
	tb := NewTokenBucket(capacity, rate)
	tb.now = c.Now
	tb.last = c.Now()
	return tb, c
}

func TestTokenBucketAllowsBurstThenThrottles(t *testing.T) {
	tb, _ := newTestTokenBucket(10, 5)

	for i := 0; i < 10; i++ {
		if !tb.Allow() {
			t.Fatalf("request %d rejected, but capacity is 10", i+1)
		}
	}
	if tb.Allow() {
		t.Error("11th request should be rejected: bucket is empty")
	}
}

func TestTokenBucketRefillsAtRate(t *testing.T) {
	tb, clock := newTestTokenBucket(10, 5)
	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	clock.Advance(time.Second) // 5 tokens/s
	if got := tb.Tokens(); got < 4.9 || got > 5.1 {
		t.Errorf("tokens after 1s = %.2f, want 5", got)
	}
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Fatalf("refilled request %d rejected", i+1)
		}
	}
	if tb.Allow() {
		t.Error("bucket should be empty again")
	}
}

func TestTokenBucketDoesNotOverfill(t *testing.T) {
	tb, clock := newTestTokenBucket(10, 5)
	clock.Advance(time.Hour)
	if got := tb.Tokens(); got != 10 {
		t.Errorf("tokens = %.2f, want capacity 10 — bucket must not accumulate unboundedly", got)
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	tb, _ := newTestTokenBucket(10, 5)
	if !tb.AllowN(7) {
		t.Fatal("AllowN(7) should succeed with 10 tokens")
	}
	if tb.AllowN(5) {
		t.Error("AllowN(5) should fail with 3 tokens left")
	}
	if !tb.AllowN(3) {
		t.Error("AllowN(3) should succeed with exactly 3 tokens")
	}
}

func TestTokenBucketConcurrentAccessNeverOverspends(t *testing.T) {
	tb := NewTokenBucket(100, 0) // no refill, so exactly 100 should pass
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 100 {
		t.Errorf("allowed %d of 500 concurrent requests, want exactly 100", allowed)
	}
}

func TestLeakyBucketSmoothsOutput(t *testing.T) {
	c := newFakeClock()
	lb := NewLeakyBucket(5, 10)
	lb.now, lb.last = c.Now, c.Now()

	for i := 0; i < 5; i++ {
		if !lb.Allow() {
			t.Fatalf("request %d rejected below capacity", i+1)
		}
	}
	if lb.Allow() {
		t.Error("bucket full, should reject")
	}

	c.Advance(100 * time.Millisecond) // leaks 1 at 10/s
	if !lb.Allow() {
		t.Error("one slot should have drained")
	}
	if lb.Allow() {
		t.Error("only one slot drained, second request should be rejected")
	}
}

// The classic fixed-window flaw: 2x the limit across a boundary.
func TestFixedWindowAllowsDoubleAtBoundary(t *testing.T) {
	c := newFakeClock()
	fw := NewFixedWindow(5, time.Minute)
	fw.now, fw.windowStart = c.Now, c.Now()

	c.Advance(59 * time.Second)
	n := 0
	for i := 0; i < 5; i++ {
		if fw.Allow() {
			n++
		}
	}
	c.Advance(2 * time.Second) // new window
	for i := 0; i < 5; i++ {
		if fw.Allow() {
			n++
		}
	}

	if n != 10 {
		t.Errorf("allowed %d, want 10 — this documents the boundary flaw", n)
	}
}

func newTestSlidingWindow(limit int, window time.Duration) (*SlidingWindow, *fakeClock) {
	c := newFakeClock()
	sw := NewSlidingWindow(limit, window)
	sw.now, sw.windowStart = c.Now, c.Now()
	return sw, c
}

// The same burst the fixed window let through must be blocked here.
func TestSlidingWindowBlocksBoundaryBurst(t *testing.T) {
	sw, c := newTestSlidingWindow(5, time.Minute)

	n := 0
	for i := 0; i < 5; i++ {
		if sw.Allow() {
			n++
		}
	}
	c.Advance(30 * time.Second) // half of the previous window still counts
	for i := 0; i < 5; i++ {
		if sw.Allow() {
			n++
		}
	}

	if n > 8 {
		t.Errorf("allowed %d, want at most 8 — sliding window should damp the burst", n)
	}
	if n < 5 {
		t.Errorf("allowed %d, want at least the first 5", n)
	}
}

func TestSlidingWindowRecoversFully(t *testing.T) {
	sw, c := newTestSlidingWindow(5, time.Minute)
	for i := 0; i < 5; i++ {
		sw.Allow()
	}

	c.Advance(2 * time.Minute) // both windows have rolled off
	n := 0
	for i := 0; i < 5; i++ {
		if sw.Allow() {
			n++
		}
	}
	if n != 5 {
		t.Errorf("allowed %d after full recovery, want 5", n)
	}
}

func TestSlidingWindowEnforcesLimitWithinOneWindow(t *testing.T) {
	sw, _ := newTestSlidingWindow(5, time.Minute)
	n := 0
	for i := 0; i < 20; i++ {
		if sw.Allow() {
			n++
		}
	}
	if n != 5 {
		t.Errorf("allowed %d, want exactly the limit of 5", n)
	}
}

func BenchmarkTokenBucketAllow(b *testing.B) {
	tb := NewTokenBucket(1000000, 1000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}
