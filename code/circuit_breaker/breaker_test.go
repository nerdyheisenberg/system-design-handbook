package main

import (
	"errors"
	"sync"
	"testing"
	"time"
)

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

var errBoom = errors.New("boom")

func fail() error { return errBoom }
func ok() error   { return nil }

func newTestBreaker(cfg Config) (*CircuitBreaker, *fakeClock) {
	c := newFakeClock()
	cb := New(cfg)
	cb.now = c.Now
	return cb, c
}

func TestStartsClosed(t *testing.T) {
	cb, _ := newTestBreaker(DefaultConfig())
	if cb.State() != StateClosed {
		t.Errorf("state = %s, want closed", cb.State())
	}
}

func TestTripsAfterThreshold(t *testing.T) {
	cb, _ := newTestBreaker(Config{FailureThreshold: 3, OpenTimeout: time.Minute})

	for i := 0; i < 2; i++ {
		cb.Call(fail)
		if cb.State() != StateClosed {
			t.Fatalf("tripped after %d failures, threshold is 3", i+1)
		}
	}
	cb.Call(fail)
	if cb.State() != StateOpen {
		t.Errorf("state = %s after 3 failures, want open", cb.State())
	}
}

// The whole purpose: an open breaker must not invoke the dependency at all.
func TestOpenCircuitDoesNotCallDependency(t *testing.T) {
	cb, _ := newTestBreaker(Config{FailureThreshold: 2, OpenTimeout: time.Minute})
	cb.Call(fail)
	cb.Call(fail)

	called := false
	err := cb.Call(func() error { called = true; return nil })

	if called {
		t.Error("dependency was invoked while the circuit was open")
	}
	if !errors.Is(err, ErrOpen) {
		t.Errorf("err = %v, want ErrOpen", err)
	}
}

// A success in the closed state must reset the count, or unrelated failures
// spread over hours would eventually trip the breaker.
func TestSuccessResetsFailureCount(t *testing.T) {
	cb, _ := newTestBreaker(Config{FailureThreshold: 3, OpenTimeout: time.Minute})
	cb.Call(fail)
	cb.Call(fail)
	cb.Call(ok)

	if cb.Failures() != 0 {
		t.Errorf("failures = %d after a success, want 0", cb.Failures())
	}
	cb.Call(fail)
	cb.Call(fail)
	if cb.State() != StateClosed {
		t.Error("breaker tripped on non-consecutive failures")
	}
}

func TestHalfOpenAfterTimeout(t *testing.T) {
	cb, c := newTestBreaker(Config{FailureThreshold: 2, OpenTimeout: 30 * time.Second})
	cb.Call(fail)
	cb.Call(fail)

	c.Advance(29 * time.Second)
	if cb.State() != StateOpen {
		t.Error("breaker left open state before the timeout elapsed")
	}
	c.Advance(2 * time.Second)
	if cb.State() != StateHalfOpen {
		t.Errorf("state = %s after timeout, want half-open", cb.State())
	}
}

func TestHalfOpenClosesAfterEnoughSuccesses(t *testing.T) {
	cb, c := newTestBreaker(Config{
		FailureThreshold: 2, SuccessThreshold: 2,
		OpenTimeout: time.Second, HalfOpenMaxCalls: 5,
	})
	cb.Call(fail)
	cb.Call(fail)
	c.Advance(2 * time.Second)

	cb.Call(ok)
	if cb.State() == StateClosed {
		t.Error("closed after 1 success, threshold is 2")
	}
	cb.Call(ok)
	if cb.State() != StateClosed {
		t.Errorf("state = %s after 2 successes, want closed", cb.State())
	}
}

// One failed probe means the dependency is still sick — reopen immediately
// rather than sending the full threshold of traffic at it again.
func TestHalfOpenReopensOnSingleFailure(t *testing.T) {
	cb, c := newTestBreaker(Config{
		FailureThreshold: 5, SuccessThreshold: 2,
		OpenTimeout: time.Second, HalfOpenMaxCalls: 5,
	})
	for i := 0; i < 5; i++ {
		cb.Call(fail)
	}
	c.Advance(2 * time.Second)

	cb.Call(ok)   // probe succeeds
	cb.Call(fail) // then one fails

	if cb.State() != StateOpen {
		t.Errorf("state = %s, want open — a failed probe must reopen", cb.State())
	}
}

// Without this cap, every waiting caller floods the dependency the instant the
// timeout expires, which is what killed it in the first place.
func TestHalfOpenLimitsConcurrentProbes(t *testing.T) {
	cb, c := newTestBreaker(Config{
		FailureThreshold: 2, SuccessThreshold: 10,
		OpenTimeout: time.Second, HalfOpenMaxCalls: 3,
	})
	cb.Call(fail)
	cb.Call(fail)
	c.Advance(2 * time.Second)

	calls := 0
	rejected := 0
	for i := 0; i < 10; i++ {
		err := cb.Call(func() error { calls++; return nil })
		if errors.Is(err, ErrOpen) {
			rejected++
		}
	}

	if calls != 3 {
		t.Errorf("dependency called %d times in half-open, want 3", calls)
	}
	if rejected != 7 {
		t.Errorf("rejected %d, want 7", rejected)
	}
}

func TestConcurrentCallsAreSafe(t *testing.T) {
	cb := New(Config{FailureThreshold: 100, OpenTimeout: time.Minute})
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				cb.Call(ok)
			} else {
				cb.Call(fail)
			}
		}(i)
	}
	wg.Wait() // -race proves the locking is correct
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{
		StateClosed: "closed", StateOpen: "open", StateHalfOpen: "half-open",
	} {
		if s.String() != want {
			t.Errorf("String() = %q, want %q", s.String(), want)
		}
	}
}
