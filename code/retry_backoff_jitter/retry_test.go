package main

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// fastPolicy keeps delays tiny so tests stay quick, with a seeded rng for
// reproducibility.
func fastPolicy() Policy {
	p := DefaultPolicy()
	p.BaseDelay = time.Millisecond
	p.MaxDelay = 10 * time.Millisecond
	p.rng = rand.New(rand.NewSource(1))
	return p
}

func TestSucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := fastPolicy().Do(context.Background(), func(int) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 — a success must not retry", calls)
	}
}

func TestRetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := fastPolicy().Do(context.Background(), func(attempt int) error {
		calls++
		if attempt < 3 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestExhaustsAndWrapsLastError(t *testing.T) {
	sentinel := errors.New("always fails")
	calls := 0

	p := fastPolicy()
	p.MaxAttempts = 4
	err := p.Do(context.Background(), func(int) error {
		calls++
		return sentinel
	})

	if calls != 4 {
		t.Errorf("calls = %d, want 4", calls)
	}
	var ex *ErrExhausted
	if !errors.As(err, &ex) {
		t.Fatalf("err = %T, want *ErrExhausted", err)
	}
	if ex.Attempts != 4 {
		t.Errorf("Attempts = %d, want 4", ex.Attempts)
	}
	if !errors.Is(err, sentinel) {
		t.Error("ErrExhausted should unwrap to the last error")
	}
}

// Retrying a 400 wastes the budget and adds load for no possible benefit.
func TestPermanentErrorStopsImmediately(t *testing.T) {
	inner := errors.New("400 bad request")
	calls := 0

	err := fastPolicy().Do(context.Background(), func(int) error {
		calls++
		return &Permanent{Err: inner}
	})

	if calls != 1 {
		t.Errorf("calls = %d, want 1 — permanent errors must not be retried", calls)
	}
	if !errors.Is(err, inner) {
		t.Errorf("err = %v, want the inner error unwrapped", err)
	}
}

func TestRetryablePredicate(t *testing.T) {
	notRetryable := errors.New("nope")
	calls := 0

	p := fastPolicy()
	p.Retryable = func(err error) bool { return !errors.Is(err, notRetryable) }
	err := p.Do(context.Background(), func(int) error {
		calls++
		return notRetryable
	})

	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if !errors.Is(err, notRetryable) {
		t.Errorf("err = %v, want the original error", err)
	}
}

func TestContextCancellationStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	p := fastPolicy()
	p.MaxAttempts = 100
	p.BaseDelay = 20 * time.Millisecond
	p.Jitter = JitterNone

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := p.Do(ctx, func(int) error {
		calls++
		return errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls > 5 {
		t.Errorf("calls = %d, cancellation should have stopped it quickly", calls)
	}
}

func TestBackoffGrowsExponentiallyAndCaps(t *testing.T) {
	p := Policy{BaseDelay: 100 * time.Millisecond, Multiplier: 2, MaxDelay: time.Second}

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		time.Second, // capped
		time.Second,
	}
	for i, w := range want {
		if got := p.Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

// The core claim: without jitter every client waits the identical time.
func TestNoJitterIsDeterministic(t *testing.T) {
	p := fastPolicy()
	p.Jitter = JitterNone

	first := p.delayFor(4, 0)
	for i := 0; i < 20; i++ {
		if got := p.delayFor(4, 0); got != first {
			t.Fatalf("JitterNone produced %v then %v", first, got)
		}
	}
}

func TestFullJitterSpreadsWithinBound(t *testing.T) {
	p := DefaultPolicy()
	p.rng = rand.New(rand.NewSource(42))
	bound := p.Backoff(4)

	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		d := p.delayFor(4, 0)
		if d < 0 || d > bound {
			t.Fatalf("delay %v outside [0, %v]", d, bound)
		}
		seen[d] = true
	}
	if len(seen) < 150 {
		t.Errorf("only %d distinct delays in 200 draws — poor spreading", len(seen))
	}
}

// Equal jitter keeps a floor, which matters when you do not want a retry to fire
// almost immediately.
func TestEqualJitterKeepsFloor(t *testing.T) {
	p := DefaultPolicy()
	p.Jitter = JitterEqual
	p.rng = rand.New(rand.NewSource(7))
	base := p.Backoff(4)

	for i := 0; i < 200; i++ {
		d := p.delayFor(4, 0)
		if d < base/2 || d > base {
			t.Fatalf("delay %v outside [%v, %v]", d, base/2, base)
		}
	}
}

func TestDecorrelatedJitterRespectsBounds(t *testing.T) {
	p := DefaultPolicy()
	p.Jitter = JitterDecorrelated
	p.rng = rand.New(rand.NewSource(9))

	prev := p.BaseDelay
	for i := 0; i < 100; i++ {
		d := p.delayFor(i+1, prev)
		if d < p.BaseDelay {
			t.Fatalf("delay %v below base %v", d, p.BaseDelay)
		}
		if d > p.MaxDelay {
			t.Fatalf("delay %v above cap %v", d, p.MaxDelay)
		}
		prev = d
	}
}

// Quantifies the thundering herd: with jitter, simultaneous retries spread out.
func TestJitterDesynchronisesClients(t *testing.T) {
	const clients = 1000
	buckets := func(j JitterKind) int {
		p := DefaultPolicy()
		p.Jitter = j
		p.rng = rand.New(rand.NewSource(3))
		seen := map[int64]bool{}
		for i := 0; i < clients; i++ {
			seen[p.delayFor(5, 0).Milliseconds()/100] = true // 100ms buckets
		}
		return len(seen)
	}

	none, full := buckets(JitterNone), buckets(JitterFull)
	if none != 1 {
		t.Errorf("no-jitter spread over %d buckets, want 1", none)
	}
	if full < 10 {
		t.Errorf("full jitter spread over only %d buckets", full)
	}
}
