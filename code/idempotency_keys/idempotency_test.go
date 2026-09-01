package main

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testBody = `{"amount":5000,"currency":"usd"}`

func countingHandler(counter *int) Handler {
	return func() (string, int, error) {
		*counter++
		return `{"charge_id":"ch_123"}`, 201, nil
	}
}

func TestFirstCallExecutes(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0

	res, err := store.Execute("key-1", testBody, countingHandler(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}
	if res.Replayed {
		t.Error("the first call should not be marked as replayed")
	}
	if res.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", res.StatusCode)
	}
}

// ⭐ The core guarantee: retries must not re-execute the side effect.
func TestRetriesDoNotReExecute(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0
	h := countingHandler(&calls)

	first, _ := store.Execute("key-1", testBody, h)
	for i := 0; i < 10; i++ {
		res, err := store.Execute("key-1", testBody, h)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if !res.Replayed {
			t.Errorf("retry %d was not marked replayed", i)
		}
		if res.Response != first.Response {
			t.Errorf("retry %d returned %q, want the stored %q", i, res.Response, first.Response)
		}
		if res.StatusCode != first.StatusCode {
			t.Errorf("retry %d status = %d, want %d", i, res.StatusCode, first.StatusCode)
		}
	}
	if calls != 1 {
		t.Errorf("handler called %d times across 11 requests, want 1", calls)
	}
}

func TestDifferentKeysExecuteIndependently(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0
	h := countingHandler(&calls)

	for i := 0; i < 5; i++ {
		if _, err := store.Execute(fmt.Sprintf("key-%d", i), testBody, h); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 5 {
		t.Errorf("handler called %d times, want 5", calls)
	}
}

// ⚠️ Without the fingerprint check the client silently receives the response to a
// different request.
func TestReusedKeyWithDifferentBodyIsRejected(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0
	h := countingHandler(&calls)

	store.Execute("key-1", testBody, h)
	_, err := store.Execute("key-1", `{"amount":99999}`, h)

	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Errorf("err = %v, want ErrFingerprintMismatch", err)
	}
	if HTTPStatus(err) != 422 {
		t.Errorf("HTTPStatus = %d, want 422", HTTPStatus(err))
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1 — the mismatched request must not run", calls)
	}
}

// ⭐ The state most implementations omit. Without it, both concurrent retries
// execute and the card is charged twice.
func TestConcurrentDuplicateGetsConflict(t *testing.T) {
	store := NewStore(time.Hour)
	var calls atomic.Int64

	started := make(chan struct{})
	release := make(chan struct{})

	var wg sync.WaitGroup
	var firstErr, secondErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, firstErr = store.Execute("race", testBody, func() (string, int, error) {
			calls.Add(1)
			close(started)
			<-release
			return "ok", 201, nil
		})
	}()

	<-started
	_, secondErr = store.Execute("race", testBody, func() (string, int, error) {
		calls.Add(1)
		return "SHOULD NOT RUN", 201, nil
	})
	close(release)
	wg.Wait()

	if firstErr != nil {
		t.Errorf("first call err = %v, want nil", firstErr)
	}
	if !errors.Is(secondErr, ErrConflict) {
		t.Errorf("second call err = %v, want ErrConflict", secondErr)
	}
	if HTTPStatus(secondErr) != 409 {
		t.Errorf("HTTPStatus = %d, want 409", HTTPStatus(secondErr))
	}
	if calls.Load() != 1 {
		t.Errorf("handler ran %d times — the in-progress state failed to prevent a double execution", calls.Load())
	}
}

// The strongest version of the test: many goroutines, one execution.
func TestHighConcurrencyExecutesExactlyOnce(t *testing.T) {
	store := NewStore(time.Hour)
	var calls atomic.Int64
	var succeeded, conflicted atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Execute("hot-key", testBody, func() (string, int, error) {
				calls.Add(1)
				time.Sleep(5 * time.Millisecond)
				return "ok", 201, nil
			})
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrConflict):
				conflicted.Add(1)
			default:
				t.Errorf("unexpected err %v", err)
			}
		}()
	}
	wg.Wait()

	if calls.Load() != 1 {
		t.Errorf("handler ran %d times under 200 concurrent requests, want exactly 1", calls.Load())
	}
	if succeeded.Load()+conflicted.Load() != 200 {
		t.Errorf("accounted for %d of 200 requests", succeeded.Load()+conflicted.Load())
	}
}

// A genuine failure should be retryable — otherwise a transient error
// permanently poisons the key.
func TestFailedRequestCanBeRetried(t *testing.T) {
	store := NewStore(time.Hour)
	attempt := 0

	h := func() (string, int, error) {
		attempt++
		if attempt == 1 {
			return "", 0, errors.New("downstream timeout")
		}
		return "recovered", 200, nil
	}

	if _, err := store.Execute("key-1", testBody, h); err == nil {
		t.Fatal("first attempt should have failed")
	}
	if rec, _ := store.Get("key-1"); rec.Status != StatusFailed {
		t.Errorf("status = %s, want failed", rec.Status)
	}

	res, err := store.Execute("key-1", testBody, h)
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if res.Response != "recovered" {
		t.Errorf("response = %q, want \"recovered\"", res.Response)
	}
	if attempt != 2 {
		t.Errorf("handler ran %d times, want 2", attempt)
	}
}

func TestMissingKeyIsRejected(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0

	_, err := store.Execute("", testBody, countingHandler(&calls))
	if !errors.Is(err, ErrKeyRequired) {
		t.Errorf("err = %v, want ErrKeyRequired", err)
	}
	if HTTPStatus(err) != 400 {
		t.Errorf("HTTPStatus = %d, want 400", HTTPStatus(err))
	}
	if calls != 0 {
		t.Error("handler should not run without a key")
	}
}

func TestExpiredKeyIsTreatedAsNew(t *testing.T) {
	store := NewStore(time.Hour)
	base := time.Now()
	store.now = func() time.Time { return base }

	calls := 0
	h := countingHandler(&calls)
	store.Execute("key-1", testBody, h)

	store.now = func() time.Time { return base.Add(2 * time.Hour) }
	res, err := store.Execute("key-1", testBody, h)
	if err != nil {
		t.Fatal(err)
	}
	if res.Replayed {
		t.Error("an expired key should execute fresh, not replay")
	}
	if calls != 2 {
		t.Errorf("handler called %d times, want 2", calls)
	}
}

// ⚠️ Without purging, the idempotency table grows without bound.
func TestPurgeRemovesExpiredRecords(t *testing.T) {
	store := NewStore(time.Hour)
	base := time.Now()
	store.now = func() time.Time { return base }

	calls := 0
	for i := 0; i < 10; i++ {
		store.Execute(fmt.Sprintf("old-%d", i), testBody, countingHandler(&calls))
	}
	store.now = func() time.Time { return base.Add(2 * time.Hour) }
	for i := 0; i < 5; i++ {
		store.Execute(fmt.Sprintf("new-%d", i), testBody, countingHandler(&calls))
	}

	if removed := store.Purge(); removed != 10 {
		t.Errorf("purged %d, want 10", removed)
	}
	if store.Len() != 5 {
		t.Errorf("Len = %d after purge, want 5", store.Len())
	}
}

func TestZeroTTLNeverExpires(t *testing.T) {
	store := NewStore(0)
	base := time.Now()
	store.now = func() time.Time { return base }

	calls := 0
	h := countingHandler(&calls)
	store.Execute("key-1", testBody, h)

	store.now = func() time.Time { return base.Add(100 * 365 * 24 * time.Hour) }
	res, _ := store.Execute("key-1", testBody, h)

	if !res.Replayed {
		t.Error("with TTL 0 the record should never expire")
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}
}

func TestGetReturnsACopy(t *testing.T) {
	store := NewStore(time.Hour)
	calls := 0
	store.Execute("key-1", testBody, countingHandler(&calls))

	rec, ok := store.Get("key-1")
	if !ok {
		t.Fatal("record not found")
	}
	rec.Response = "mutated"

	again, _ := store.Get("key-1")
	if again.Response == "mutated" {
		t.Error("Get returned a pointer into the store; callers can corrupt it")
	}
}

func TestGetMissingKey(t *testing.T) {
	if _, ok := NewStore(time.Hour).Get("nope"); ok {
		t.Error("Get should report a missing key")
	}
}

func TestFingerprintIsStableAndDistinct(t *testing.T) {
	if Fingerprint(testBody) != Fingerprint(testBody) {
		t.Error("Fingerprint is not deterministic")
	}
	if Fingerprint(testBody) == Fingerprint(testBody+" ") {
		t.Error("different bodies produced the same fingerprint")
	}
}

func TestStatusString(t *testing.T) {
	for s, want := range map[Status]string{
		StatusInProgress: "in_progress", StatusCompleted: "completed", StatusFailed: "failed",
	} {
		if s.String() != want {
			t.Errorf("String() = %q, want %q", s.String(), want)
		}
	}
}

func TestHTTPStatusMapping(t *testing.T) {
	cases := map[error]int{
		nil:                    200,
		ErrKeyRequired:         400,
		ErrConflict:            409,
		ErrFingerprintMismatch: 422,
		errors.New("boom"):     500,
	}
	for err, want := range cases {
		if got := HTTPStatus(err); got != want {
			t.Errorf("HTTPStatus(%v) = %d, want %d", err, got, want)
		}
	}
}

// A slow handler must not block unrelated keys.
func TestSlowHandlerDoesNotBlockOtherKeys(t *testing.T) {
	store := NewStore(time.Hour)
	release := make(chan struct{})
	started := make(chan struct{})

	go func() {
		store.Execute("slow", testBody, func() (string, int, error) {
			close(started)
			<-release
			return "ok", 200, nil
		})
	}()
	<-started

	done := make(chan struct{})
	go func() {
		defer close(done)
		store.Execute("fast", testBody, func() (string, int, error) { return "ok", 200, nil })
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("a slow handler blocked an unrelated key — the lock is held during execution")
	}
	close(release)
}
