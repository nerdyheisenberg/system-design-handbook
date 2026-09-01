// Package main implements four rate limiters. See Chapter 23.
//
//	Token bucket   — allows bursts up to the bucket size, then a steady rate.
//	Leaky bucket   — smooths output to a fixed rate, no bursts.
//	Fixed window   — trivial, but permits 2x the limit across a boundary.
//	Sliding window — the fixed-window boundary problem fixed, cheaply.
//
// All are single-node. Distributing them is the interesting problem (Chapter 24,
// design 3): local counters with async reconciliation beat a synchronous central
// store, because a rate limiter is defensive and should not be on the hot path.
package main

import (
	"fmt"
	"sync"
	"time"
)

// TokenBucket refills continuously and allows a burst up to capacity.
// This is what most public APIs actually implement.
type TokenBucket struct {
	mu         sync.Mutex
	capacity   float64
	tokens     float64
	refillRate float64 // tokens per second
	last       time.Time
	now        func() time.Time
}

func NewTokenBucket(capacity int, refillPerSecond float64) *TokenBucket {
	return &TokenBucket{
		capacity:   float64(capacity),
		tokens:     float64(capacity),
		refillRate: refillPerSecond,
		last:       time.Now(),
		now:        time.Now,
	}
}

func (tb *TokenBucket) Allow() bool { return tb.AllowN(1) }

func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}

// Tokens reports the current allowance, for tests and for the
// RateLimit-Remaining header.
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

func (tb *TokenBucket) refill() {
	now := tb.now()
	elapsed := now.Sub(tb.last).Seconds()
	tb.last = now
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
}

// LeakyBucket models a queue draining at a fixed rate. Unlike a token bucket it
// never allows a burst through: output is perfectly smooth.
type LeakyBucket struct {
	mu       sync.Mutex
	capacity float64
	level    float64
	leakRate float64 // units per second
	last     time.Time
	now      func() time.Time
}

func NewLeakyBucket(capacity int, leakPerSecond float64) *LeakyBucket {
	return &LeakyBucket{
		capacity: float64(capacity),
		leakRate: leakPerSecond,
		last:     time.Now(),
		now:      time.Now,
	}
}

func (lb *LeakyBucket) Allow() bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := lb.now()
	lb.level -= now.Sub(lb.last).Seconds() * lb.leakRate
	lb.last = now
	if lb.level < 0 {
		lb.level = 0
	}
	if lb.level+1 <= lb.capacity {
		lb.level++
		return true
	}
	return false
}

// FixedWindow counts requests per wall-clock window.
//
// The flaw worth knowing: a client can send `limit` requests at the end of one
// window and `limit` more at the start of the next, so a 100/min limiter permits
// 200 requests within a 2-second span.
type FixedWindow struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	count       int
	windowStart time.Time
	now         func() time.Time
}

func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	return &FixedWindow{limit: limit, window: window, windowStart: time.Now(), now: time.Now}
}

func (fw *FixedWindow) Allow() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := fw.now()
	if now.Sub(fw.windowStart) >= fw.window {
		fw.count = 0
		fw.windowStart = now
	}
	if fw.count < fw.limit {
		fw.count++
		return true
	}
	return false
}

// SlidingWindow interpolates the previous window's count by how much of it still
// overlaps the trailing period. It costs two counters instead of a timestamp log,
// and eliminates the fixed-window boundary burst.
type SlidingWindow struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	current     int
	previous    int
	windowStart time.Time
	now         func() time.Time
}

func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{limit: limit, window: window, windowStart: time.Now(), now: time.Now}
}

func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := sw.now()
	elapsed := now.Sub(sw.windowStart)
	switch {
	case elapsed >= 2*sw.window:
		sw.previous, sw.current = 0, 0
		sw.windowStart = now
		elapsed = 0
	case elapsed >= sw.window:
		sw.previous, sw.current = sw.current, 0
		sw.windowStart = sw.windowStart.Add(sw.window)
		elapsed -= sw.window
	}

	overlap := 1 - float64(elapsed)/float64(sw.window)
	estimated := float64(sw.previous)*overlap + float64(sw.current)
	if estimated < float64(sw.limit) {
		sw.current++
		return true
	}
	return false
}

func main() {
	fmt.Println("token bucket: capacity 10, refill 5/s")
	tb := NewTokenBucket(10, 5)
	allowed := 0
	for i := 0; i < 20; i++ {
		if tb.Allow() {
			allowed++
		}
	}
	fmt.Printf("  burst of 20 -> %d allowed (the capacity), %d rejected\n", allowed, 20-allowed)

	time.Sleep(400 * time.Millisecond)
	fmt.Printf("  after 400ms -> %.1f tokens refilled\n", tb.Tokens())

	fmt.Println("\nfixed window: 5 per 100ms — note the boundary problem")
	fw := NewFixedWindow(5, 100*time.Millisecond)
	n := 0
	for i := 0; i < 5; i++ {
		if fw.Allow() {
			n++
		}
	}
	time.Sleep(105 * time.Millisecond)
	for i := 0; i < 5; i++ {
		if fw.Allow() {
			n++
		}
	}
	fmt.Printf("  %d requests allowed across the boundary — 2x the nominal limit\n", n)

	fmt.Println("\nsliding window: 5 per 100ms — boundary handled")
	sw := NewSlidingWindow(5, 100*time.Millisecond)
	n = 0
	for i := 0; i < 5; i++ {
		if sw.Allow() {
			n++
		}
	}
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		if sw.Allow() {
			n++
		}
	}
	fmt.Printf("  %d allowed — the previous window is still weighted in\n", n)
}
