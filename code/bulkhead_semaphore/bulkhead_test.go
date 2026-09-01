package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllowsUpToCapacity(t *testing.T) {
	b := NewBulkhead("t", 3)
	release := make(chan struct{})
	var running atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.TryDo(func() error {
				running.Add(1)
				<-release
				return nil
			})
		}()
	}

	waitFor(t, func() bool { return running.Load() == 3 })
	if err := b.TryDo(func() error { return nil }); !errors.Is(err, ErrFull) {
		t.Errorf("err = %v, want ErrFull once capacity is taken", err)
	}

	close(release)
	wg.Wait()
}

// Concurrency must never exceed capacity — that is the entire guarantee.
func TestNeverExceedsCapacity(t *testing.T) {
	b := NewBulkhead("t", 5)
	var concurrent, peak atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			b.Do(ctx, func() error {
				c := concurrent.Add(1)
				for {
					p := peak.Load()
					if c <= p || peak.CompareAndSwap(p, c) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				concurrent.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()

	if peak.Load() > 5 {
		t.Errorf("peak concurrency = %d, exceeded capacity 5", peak.Load())
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrency = %d, suspiciously serialised", peak.Load())
	}
}

func TestSlotsReleasedAfterCompletion(t *testing.T) {
	b := NewBulkhead("t", 2)
	for i := 0; i < 100; i++ {
		if err := b.TryDo(func() error { return nil }); err != nil {
			t.Fatalf("call %d rejected: %v — slots are leaking", i, err)
		}
	}
	if b.Active() != 0 {
		t.Errorf("Active = %d after all calls returned, want 0", b.Active())
	}
}

// A panicking handler must not permanently consume a slot.
func TestSlotReleasedOnPanic(t *testing.T) {
	b := NewBulkhead("t", 1)

	func() {
		defer func() { recover() }()
		b.TryDo(func() error { panic("boom") })
	}()

	if err := b.TryDo(func() error { return nil }); err != nil {
		t.Errorf("slot was leaked by a panicking call: %v", err)
	}
}

func TestErrorsPropagate(t *testing.T) {
	b := NewBulkhead("t", 1)
	sentinel := errors.New("inner")
	if err := b.TryDo(func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the wrapped error", err)
	}
}

func TestDoWaitsForASlot(t *testing.T) {
	b := NewBulkhead("t", 1)
	release := make(chan struct{})
	started := make(chan struct{})

	go func() {
		b.TryDo(func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- b.Do(ctx, func() error { return nil })
	}()

	select {
	case <-done:
		t.Fatal("Do returned while the bulkhead was full")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Errorf("Do err = %v after a slot freed up", err)
	}
}

func TestDoTimesOutWhenFull(t *testing.T) {
	b := NewBulkhead("t", 1)
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		b.TryDo(func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := b.Do(ctx, func() error { return nil })
	elapsed := time.Since(start)

	if !errors.Is(err, ErrFull) {
		t.Errorf("err = %v, want ErrFull", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("waited %v, should have given up at the deadline", elapsed)
	}
	close(release)
}

// The point of the pattern: saturating one bulkhead must not affect another.
func TestBulkheadsAreIsolated(t *testing.T) {
	slow := NewBulkhead("slow", 2)
	fast := NewBulkhead("fast", 10)

	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slow.TryDo(func() error { <-release; return nil })
		}()
	}
	waitFor(t, func() bool { return slow.Active() == 2 })

	for i := 0; i < 10; i++ {
		if err := fast.TryDo(func() error { return nil }); err != nil {
			t.Fatalf("fast bulkhead rejected call %d while slow was saturated: %v", i, err)
		}
	}

	close(release)
	wg.Wait()
}

func TestCountersAreAccurate(t *testing.T) {
	b := NewBulkhead("t", 1)
	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		b.TryDo(func() error { close(started); <-release; return nil })
	}()
	<-started

	for i := 0; i < 5; i++ {
		b.TryDo(func() error { return nil })
	}
	close(release)

	if b.Rejected() != 5 {
		t.Errorf("Rejected = %d, want 5", b.Rejected())
	}
	if b.Accepted() != 1 {
		t.Errorf("Accepted = %d, want 1", b.Accepted())
	}
}

func TestZeroCapacityPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("capacity 0 should panic")
		}
	}()
	NewBulkhead("t", 0)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
