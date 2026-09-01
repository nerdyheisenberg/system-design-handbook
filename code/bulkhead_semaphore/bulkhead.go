// Package main implements the bulkhead pattern with a counting semaphore.
// See Chapter 16.
//
// The failure it prevents: one slow dependency consumes every worker in the
// process, so requests that never touch that dependency also fail. Partitioning
// concurrency per dependency means a slow payment provider cannot take down the
// login endpoint.
//
// The name is from ships: a hull is divided into compartments so one breach
// floods one compartment rather than the whole vessel.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrFull is returned when no slot is available within the caller's deadline.
var ErrFull = errors.New("bulkhead: no capacity")

type Bulkhead struct {
	name string
	// slots is a buffered channel used as a counting semaphore: capacity is the
	// buffer size, so a send blocks exactly when the bulkhead is full.
	slots chan struct{}

	active   atomic.Int64
	accepted atomic.Int64
	rejected atomic.Int64
}

func NewBulkhead(name string, capacity int) *Bulkhead {
	if capacity <= 0 {
		panic("bulkhead: capacity must be positive")
	}
	return &Bulkhead{name: name, slots: make(chan struct{}, capacity)}
}

// Do runs fn if a slot is free, otherwise waits until ctx expires and returns
// ErrFull. ⚠️ Callers must pass a context with a timeout; an unbounded wait here
// recreates the exhaustion the bulkhead exists to prevent.
func (b *Bulkhead) Do(ctx context.Context, fn func() error) error {
	select {
	case b.slots <- struct{}{}:
	case <-ctx.Done():
		b.rejected.Add(1)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ErrFull
		}
		return ctx.Err()
	}

	b.accepted.Add(1)
	b.active.Add(1)
	defer func() {
		b.active.Add(-1)
		<-b.slots
	}()
	return fn()
}

// TryDo runs fn only if a slot is immediately available. Use this on paths where
// shedding load is better than queueing.
func (b *Bulkhead) TryDo(fn func() error) error {
	select {
	case b.slots <- struct{}{}:
	default:
		b.rejected.Add(1)
		return ErrFull
	}

	b.accepted.Add(1)
	b.active.Add(1)
	defer func() {
		b.active.Add(-1)
		<-b.slots
	}()
	return fn()
}

func (b *Bulkhead) Capacity() int   { return cap(b.slots) }
func (b *Bulkhead) Active() int64   { return b.active.Load() }
func (b *Bulkhead) Accepted() int64 { return b.accepted.Load() }
func (b *Bulkhead) Rejected() int64 { return b.rejected.Load() }

func main() {
	// Two dependencies with separate compartments. Payments is slow and will
	// saturate its own pool; auth must be unaffected.
	payments := NewBulkhead("payments", 5)
	auth := NewBulkhead("auth", 20)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			payments.Do(ctx, func() error {
				time.Sleep(100 * time.Millisecond) // the slow dependency
				return nil
			})
		}()
	}

	time.Sleep(10 * time.Millisecond) // let payments saturate

	authOK := 0
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := auth.Do(ctx, func() error { return nil }); err == nil {
				mu.Lock()
				authOK++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	fmt.Printf("payments (capacity %d): %d accepted, %d shed\n",
		payments.Capacity(), payments.Accepted(), payments.Rejected())
	fmt.Printf("auth     (capacity %d): %d accepted, %d shed\n",
		auth.Capacity(), auth.Accepted(), auth.Rejected())
	fmt.Printf("\n%d/20 auth requests succeeded while payments was saturated\n", authOK)
	fmt.Println("without bulkheads, all 70 would share one pool and auth would starve")
}
