// Package main implements retry with exponential backoff and jitter. See Chapter 16.
//
// Two things matter more than the retry loop itself:
//
//  1. Jitter. Without it, every client that failed at the same moment retries at
//     the same moment, so the retry wave is as synchronised as the failure was.
//     This is the thundering herd, and it is why a recovering service immediately
//     falls over again.
//
//  2. Retrying only what is retryable. Retrying a 400 wastes budget and adds load;
//     retrying a non-idempotent write can duplicate side effects.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// Multiplier is the growth factor per attempt, conventionally 2.
	Multiplier float64
	// Jitter selects the randomisation strategy.
	Jitter JitterKind
	// Retryable decides whether an error is worth another attempt.
	// Nil means retry everything, which is rarely what you want.
	Retryable func(error) bool

	rng *rand.Rand
}

type JitterKind int

const (
	// JitterNone is deterministic backoff. Correct delays, synchronised clients.
	JitterNone JitterKind = iota
	// JitterFull picks uniformly in [0, backoff]. The AWS recommendation, and
	// the best at spreading load.
	JitterFull
	// JitterEqual picks in [backoff/2, backoff]. Keeps a latency floor while
	// still de-synchronising.
	JitterEqual
	// JitterDecorrelated walks the delay based on the previous one, which
	// spreads work slightly better than full jitter over many attempts.
	JitterDecorrelated
)

func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Multiplier:  2,
		Jitter:      JitterFull,
	}
}

// ErrExhausted wraps the last error when every attempt failed.
type ErrExhausted struct {
	Attempts int
	Last     error
}

func (e *ErrExhausted) Error() string {
	return fmt.Sprintf("retry: gave up after %d attempts: %v", e.Attempts, e.Last)
}
func (e *ErrExhausted) Unwrap() error { return e.Last }

// Permanent marks an error as not worth retrying, e.g. a 400 or a validation
// failure. Do returns it immediately without consuming further attempts.
type Permanent struct{ Err error }

func (p *Permanent) Error() string { return "permanent: " + p.Err.Error() }
func (p *Permanent) Unwrap() error { return p.Err }

// Do runs fn until it succeeds, the policy is exhausted, or ctx is cancelled.
func (p Policy) Do(ctx context.Context, fn func(attempt int) error) error {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.Multiplier <= 0 {
		p.Multiplier = 2
	}

	var last error
	prevDelay := p.BaseDelay

	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn(attempt)
		if err == nil {
			return nil
		}
		last = err

		var perm *Permanent
		if errors.As(err, &perm) {
			return perm.Err
		}
		if p.Retryable != nil && !p.Retryable(err) {
			return err
		}
		if attempt == p.MaxAttempts {
			break
		}

		delay := p.delayFor(attempt, prevDelay)
		prevDelay = delay

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return &ErrExhausted{Attempts: p.MaxAttempts, Last: last}
}

// Backoff is the un-jittered delay before the given attempt (1-based), capped.
func (p Policy) Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(p.BaseDelay) * math.Pow(p.Multiplier, float64(attempt-1))
	if p.MaxDelay > 0 && d > float64(p.MaxDelay) {
		return p.MaxDelay
	}
	return time.Duration(d)
}

func (p Policy) delayFor(attempt int, prev time.Duration) time.Duration {
	base := p.Backoff(attempt)
	r := p.rng
	random := rand.Float64
	if r != nil {
		random = r.Float64
	}

	var d time.Duration
	switch p.Jitter {
	case JitterNone:
		d = base
	case JitterFull:
		d = time.Duration(random() * float64(base))
	case JitterEqual:
		d = base/2 + time.Duration(random()*float64(base/2))
	case JitterDecorrelated:
		d = time.Duration(random() * float64(3*prev))
		if d < p.BaseDelay {
			d = p.BaseDelay
		}
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	return d
}

func main() {
	p := DefaultPolicy()

	fmt.Println("un-jittered backoff (base 100ms, x2, cap 30s):")
	for a := 1; a <= 8; a++ {
		fmt.Printf("  attempt %d: %v\n", a, p.Backoff(a))
	}

	fmt.Println("\nwhy jitter matters — 10 clients retrying attempt 4 (nominal 800ms):")
	fmt.Print("  no jitter:   ")
	np := p
	np.Jitter = JitterNone
	for i := 0; i < 10; i++ {
		fmt.Printf("%4dms ", np.delayFor(4, 0).Milliseconds())
	}
	fmt.Println("\n  ^ all ten hit the recovering service in the same millisecond")

	fmt.Print("  full jitter: ")
	for i := 0; i < 10; i++ {
		fmt.Printf("%4dms ", p.delayFor(4, 0).Milliseconds())
	}
	fmt.Println("\n  ^ spread across the whole window")

	attempts := 0
	err := p.Do(context.Background(), func(attempt int) error {
		attempts++
		if attempt < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	fmt.Printf("\ntransient failure: succeeded on attempt %d, err=%v\n", attempts, err)

	attempts = 0
	err = p.Do(context.Background(), func(attempt int) error {
		attempts++
		return &Permanent{Err: errors.New("400 bad request")}
	})
	fmt.Printf("permanent failure: stopped after %d attempt, err=%v\n", attempts, err)
}
