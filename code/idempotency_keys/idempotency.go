// Package main implements idempotent request handling. See Chapter 10.
//
// Why this is necessary: a client that times out cannot know whether the request
// succeeded. The response may have been lost on the way back. So it retries — and
// without idempotency, the customer is charged twice.
//
// The full state machine, including the state most implementations omit:
//
//	new key                       → execute, store the response, return it
//	same key + same body, done    → return the STORED response (do not re-execute)
//	same key + same body, running → 409 Conflict (a concurrent duplicate)
//	same key + DIFFERENT body     → 422 Unprocessable (the client has a bug)
//	expired key                   → treat as new
//
// ⚠️ The in-progress state is what most implementations get wrong. Without it,
// two concurrent retries both see "no record", both execute, and you have
// charged the card twice — exactly the failure the mechanism was meant to
// prevent.
//
// ⚠️ And the body fingerprint matters: without it, a client reusing a key for a
// different request silently receives the wrong response.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Status int

const (
	StatusInProgress Status = iota
	StatusCompleted
	StatusFailed
)

func (s Status) String() string {
	switch s {
	case StatusInProgress:
		return "in_progress"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	}
	return "unknown"
}

type Record struct {
	Key         string
	Fingerprint string
	Status      Status
	Response    string
	StatusCode  int
	CreatedAt   time.Time
	CompletedAt *time.Time
}

var (
	// ErrConflict maps to HTTP 409: the same request is already running.
	ErrConflict = errors.New("idempotency: request already in progress")
	// ErrFingerprintMismatch maps to HTTP 422: the key was reused with a
	// different body, which is a client bug and must not be silently accepted.
	ErrFingerprintMismatch = errors.New("idempotency: key reused with a different request body")
	// ErrKeyRequired maps to HTTP 400.
	ErrKeyRequired = errors.New("idempotency: key is required for this operation")
)

type Store struct {
	mu      sync.Mutex
	records map[string]*Record
	ttl     time.Duration
	now     func() time.Time
}

func NewStore(ttl time.Duration) *Store {
	return &Store{
		records: map[string]*Record{},
		ttl:     ttl,
		now:     time.Now,
	}
}

// Fingerprint hashes the request body so a reused key with different content is
// detected rather than silently served the wrong response.
func Fingerprint(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// Handler executes the real work. It runs at most once per idempotency key.
type Handler func() (response string, statusCode int, err error)

// Result is what the caller returns to the client.
type Result struct {
	Response   string
	StatusCode int
	// Replayed is true when this came from the store rather than a fresh
	// execution. Worth exposing as a metric: a high replay rate means clients
	// are timing out.
	Replayed bool
}

// Execute runs handler at most once for the given key.
func (s *Store) Execute(key, body string, handler Handler) (Result, error) {
	if key == "" {
		return Result{}, ErrKeyRequired
	}
	fingerprint := Fingerprint(body)

	// Phase 1: claim the key, or return what already exists.
	s.mu.Lock()
	existing, ok := s.records[key]
	if ok && s.expired(existing) {
		delete(s.records, key)
		ok = false
	}
	if ok {
		if existing.Fingerprint != fingerprint {
			s.mu.Unlock()
			return Result{}, ErrFingerprintMismatch
		}
		switch existing.Status {
		case StatusInProgress:
			// ⭐ A concurrent duplicate. Rejecting is correct: executing would
			// double-charge, and blocking would tie up a connection.
			s.mu.Unlock()
			return Result{}, ErrConflict
		case StatusCompleted:
			res := Result{
				Response:   existing.Response,
				StatusCode: existing.StatusCode,
				Replayed:   true,
			}
			s.mu.Unlock()
			return res, nil
		case StatusFailed:
			// A previous attempt failed. Allow a genuine retry by re-claiming.
			existing.Status = StatusInProgress
			existing.CreatedAt = s.now()
		}
	} else {
		s.records[key] = &Record{
			Key:         key,
			Fingerprint: fingerprint,
			Status:      StatusInProgress,
			CreatedAt:   s.now(),
		}
	}
	// ⚠️ Unlock before executing. Holding the lock across the handler would
	// serialise every unrelated key behind one slow request.
	s.mu.Unlock()

	// Phase 2: execute outside the lock, so a slow handler does not block others.
	response, statusCode, err := handler()

	// Phase 3: record the outcome.
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.records[key]
	if rec == nil {
		return Result{}, errors.New("idempotency: record vanished during execution")
	}
	if err != nil {
		rec.Status = StatusFailed
		return Result{}, err
	}
	now := s.now()
	rec.Status = StatusCompleted
	rec.Response = response
	rec.StatusCode = statusCode
	rec.CompletedAt = &now

	return Result{Response: response, StatusCode: statusCode}, nil
}

func (s *Store) expired(r *Record) bool {
	return s.ttl > 0 && s.now().Sub(r.CreatedAt) > s.ttl
}

func (s *Store) Get(key string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[key]
	if !ok || s.expired(r) {
		return nil, false
	}
	copy := *r
	return &copy, true
}

// Purge removes expired records. In production this is a scheduled job or a
// database TTL index — without it the table grows forever.
func (s *Store) Purge() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for k, r := range s.records {
		if s.expired(r) {
			delete(s.records, k)
			removed++
		}
	}
	return removed
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// HTTPStatus maps an error to the status code a real API should return.
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return 200
	case errors.Is(err, ErrKeyRequired):
		return 400
	case errors.Is(err, ErrConflict):
		return 409
	case errors.Is(err, ErrFingerprintMismatch):
		return 422
	default:
		return 500
	}
}

func main() {
	store := NewStore(24 * time.Hour)
	charges := 0

	chargeCard := func() (string, int, error) {
		charges++
		return `{"charge_id":"ch_123","status":"succeeded"}`, 201, nil
	}

	const key = "idem-key-abc123"
	const body = `{"amount":5000,"currency":"usd"}`

	fmt.Println("=== the client times out and retries three times ===")
	for i := 1; i <= 3; i++ {
		res, err := store.Execute(key, body, chargeCard)
		fmt.Printf("  attempt %d: replayed=%-5v status=%d err=%v\n",
			i, res.Replayed, res.StatusCode, err)
	}
	fmt.Printf("⭐ the card was charged %d time(s), not 3\n", charges)

	fmt.Println("\n=== the key is reused with a different body ===")
	_, err := store.Execute(key, `{"amount":99999,"currency":"usd"}`, chargeCard)
	fmt.Printf("  err=%v -> HTTP %d\n", err, HTTPStatus(err))
	fmt.Printf("  charges still %d — a client bug must not be silently honoured\n", charges)

	fmt.Println("\n=== two concurrent retries race ===")
	store2 := NewStore(time.Hour)
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, results[0] = store2.Execute("race-key", body, func() (string, int, error) {
			close(started)
			<-release
			return "ok", 201, nil
		})
	}()

	<-started
	_, results[1] = store2.Execute("race-key", body, func() (string, int, error) {
		return "SHOULD NOT RUN", 201, nil
	})
	close(release)
	wg.Wait()

	fmt.Printf("  first:  err=%v\n", results[0])
	fmt.Printf("  second: err=%v -> HTTP %d\n", results[1], HTTPStatus(results[1]))
	fmt.Println("⭐ the in-progress state is what prevents the double charge here")

	fmt.Println("\n=== expiry ===")
	store3 := NewStore(50 * time.Millisecond)
	store3.Execute("expiring", body, chargeCard)
	fmt.Printf("  records: %d\n", store3.Len())
	time.Sleep(80 * time.Millisecond)
	fmt.Printf("  purged: %d, remaining: %d\n", store3.Purge(), store3.Len())
	fmt.Println("  ⚠️ without a purge job the idempotency table grows forever")
}
