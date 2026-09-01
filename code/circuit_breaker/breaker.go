// Package main implements a three-state circuit breaker. See Chapter 16.
//
// The point is not to make failures faster. It is to stop a struggling dependency
// from being hammered by retries while it tries to recover, and to stop callers
// from holding threads open waiting on something already known to be broken.
//
//	closed    → calls pass through; consecutive failures are counted
//	open      → calls fail immediately without touching the dependency
//	half-open → a limited number of probes decide whether to close again
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// ErrOpen is returned without invoking the wrapped call.
var ErrOpen = errors.New("circuit breaker is open")

type Config struct {
	// FailureThreshold is the number of consecutive failures that trips the breaker.
	FailureThreshold int
	// SuccessThreshold is how many probes must succeed in half-open before closing.
	SuccessThreshold int
	// OpenTimeout is how long to stay open before allowing probes.
	OpenTimeout time.Duration
	// HalfOpenMaxCalls caps concurrent probes so a still-broken dependency is not
	// flooded the moment the timeout expires.
	HalfOpenMaxCalls int
}

func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		OpenTimeout:      30 * time.Second,
		HalfOpenMaxCalls: 3,
	}
}

type CircuitBreaker struct {
	mu     sync.Mutex
	cfg    Config
	state  State
	now    func() time.Time
	onTrip func(from, to State)

	failures     int
	successes    int
	openedAt     time.Time
	halfOpenCall int
}

func New(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 1
	}
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	return &CircuitBreaker{cfg: cfg, state: StateClosed, now: time.Now}
}

// Call runs fn unless the circuit is open. It returns ErrOpen without calling fn
// when the circuit is open or half-open probes are exhausted.
func (cb *CircuitBreaker) Call(fn func() error) error {
	if err := cb.beforeCall(); err != nil {
		return err
	}
	err := fn()
	cb.afterCall(err)
	return err
}

func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateOpen {
		if cb.now().Sub(cb.openedAt) < cb.cfg.OpenTimeout {
			return ErrOpen
		}
		cb.transition(StateHalfOpen)
	}
	if cb.state == StateHalfOpen {
		if cb.halfOpenCall >= cb.cfg.HalfOpenMaxCalls {
			return ErrOpen
		}
		cb.halfOpenCall++
	}
	return nil
}

func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.successes = 0
		// A single failure in half-open reopens: the dependency is still sick.
		if cb.state == StateHalfOpen || cb.failures >= cb.cfg.FailureThreshold {
			cb.transition(StateOpen)
		}
		return
	}

	cb.failures = 0
	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.cfg.SuccessThreshold {
			cb.transition(StateClosed)
		}
	}
}

func (cb *CircuitBreaker) transition(to State) {
	if cb.state == to {
		return
	}
	from := cb.state
	cb.state = to
	switch to {
	case StateOpen:
		cb.openedAt = cb.now()
		cb.successes, cb.halfOpenCall = 0, 0
	case StateHalfOpen:
		cb.successes, cb.halfOpenCall = 0, 0
	case StateClosed:
		cb.failures, cb.successes, cb.halfOpenCall = 0, 0, 0
	}
	if cb.onTrip != nil {
		cb.onTrip(from, to)
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	// Report half-open once the timeout has expired, even before the next call.
	if cb.state == StateOpen && cb.now().Sub(cb.openedAt) >= cb.cfg.OpenTimeout {
		return StateHalfOpen
	}
	return cb.state
}

func (cb *CircuitBreaker) Failures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}

func main() {
	cb := New(Config{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenTimeout:      200 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	})
	cb.onTrip = func(from, to State) { fmt.Printf("    [%s -> %s]\n", from, to) }

	failing := func() error { return errors.New("connection refused") }
	healthy := func() error { return nil }

	fmt.Println("dependency starts failing:")
	for i := 1; i <= 5; i++ {
		err := cb.Call(failing)
		fmt.Printf("  call %d: %v\n", i, err)
	}
	fmt.Println("  ^ calls 4 and 5 never touched the dependency")

	fmt.Println("\nwaiting for the open timeout...")
	time.Sleep(220 * time.Millisecond)

	fmt.Println("dependency recovers:")
	for i := 1; i <= 3; i++ {
		err := cb.Call(healthy)
		fmt.Printf("  probe %d: err=%v state=%s\n", i, err, cb.State())
	}
}
